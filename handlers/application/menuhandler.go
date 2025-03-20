package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"gopkg.in/leonelquinteros/gotext.v1"

	"git.defalsify.org/vise.git/asm"
	"git.defalsify.org/vise.git/cache"
	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/lang"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/persist"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"
	"git.grassecon.net/grassrootseconomics/common/hex"
	"git.grassecon.net/grassrootseconomics/common/identity"
	commonlang "git.grassecon.net/grassrootseconomics/common/lang"
	"git.grassecon.net/grassrootseconomics/common/person"
	"git.grassecon.net/grassrootseconomics/common/phone"
	"git.grassecon.net/grassrootseconomics/common/pin"
	"git.grassecon.net/grassrootseconomics/sarafu-api/models"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/profile"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

var (
	logg           = logging.NewVanilla().WithDomain("ussdmenuhandler").WithContextKey("SessionId")
	scriptDir      = path.Join("services", "registration")
	translationDir = path.Join(scriptDir, "locale")
)

// TODO: this is only in use in testing, should be moved to test domain and/or replaced by asm.FlagParser
// FlagManager handles centralized flag management
type FlagManager struct {
	*asm.FlagParser
}

// NewFlagManager creates a new FlagManager instance
func NewFlagManager(csvPath string) (*FlagManager, error) {
	parser := asm.NewFlagParser()
	_, err := parser.Load(csvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load flag parser: %v", err)
	}

	return &FlagManager{
		FlagParser: parser,
	}, nil
}

func (fm *FlagManager) SetDebug() {
	fm.FlagParser = fm.FlagParser.WithDebug()
}

// GetFlag retrieves a flag value by its label
func (fm *FlagManager) GetFlag(label string) (uint32, error) {
	return fm.FlagParser.GetFlag(label)
}

type MenuHandlers struct {
	pe                   *persist.Persister
	st                   *state.State
	ca                   cache.Memory
	userdataStore        store.DataStore
	flagManager          *FlagManager
	accountService       remote.AccountService
	prefixDb             storedb.PrefixDb
	profile              *profile.Profile
	ReplaceSeparatorFunc func(string) string
}

// NewHandlers creates a new instance of the Handlers struct with the provided dependencies.
func NewMenuHandlers(appFlags *FlagManager, userdataStore db.Db, accountService remote.AccountService, replaceSeparatorFunc func(string) string) (*MenuHandlers, error) {
	if userdataStore == nil {
		return nil, fmt.Errorf("cannot create handler with nil userdata store")
	}
	userDb := &store.UserDataStore{
		Db: userdataStore,
	}

	// Instantiate the SubPrefixDb with "DATATYPE_USERDATA" prefix
	prefix := storedb.ToBytes(db.DATATYPE_USERDATA)
	prefixDb := storedb.NewSubPrefixDb(userdataStore, prefix)

	h := &MenuHandlers{
		userdataStore:        userDb,
		flagManager:          appFlags,
		accountService:       accountService,
		prefixDb:             prefixDb,
		profile:              &profile.Profile{Max: 6},
		ReplaceSeparatorFunc: replaceSeparatorFunc,
	}
	return h, nil
}

// SetPersister sets persister instance to the handlers.
func (h *MenuHandlers) SetPersister(pe *persist.Persister) {
	if h.pe != nil {
		panic("persister already set")
	}
	h.pe = pe
}

// Init initializes the handler for a new session.
func (h *MenuHandlers) Init(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var r resource.Result
	if h.pe == nil {
		logg.WarnCtxf(ctx, "handler init called before it is ready or more than once", "state", h.st, "cache", h.ca)
		return r, nil
	}
	defer func() {
		h.Exit()
	}()

	h.st = h.pe.GetState()
	h.ca = h.pe.GetMemory()
	sessionId, ok := ctx.Value("SessionId").(string)
	if ok {
		ctx = context.WithValue(ctx, "SessionId", sessionId)
	}

	if h.st == nil || h.ca == nil {
		logg.ErrorCtxf(ctx, "perister fail in handler", "state", h.st, "cache", h.ca)
		return r, fmt.Errorf("cannot get state and memory for handler")
	}

	logg.DebugCtxf(ctx, "handler has been initialized", "state", h.st, "cache", h.ca)

	return r, nil
}

func (h *MenuHandlers) Exit() {
	h.pe = nil
}

// retrieves language codes from the context that can be used for handling translations.
func codeFromCtx(ctx context.Context) string {
	var code string
	if ctx.Value("Language") != nil {
		lang := ctx.Value("Language").(lang.Language)
		code = lang.Code
	}
	return code
}

// SetLanguage sets the language across the menu.
func (h *MenuHandlers) SetLanguage(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	symbol, _ := h.st.Where()
	code := strings.Split(symbol, "_")[1]

	// TODO: Use defaultlanguage from config
	if !commonlang.IsValidISO639(code) {
		//Fallback to english instead?
		code = "eng"
	}
	err := h.persistLanguageCode(ctx, code)
	if err != nil {
		return res, err
	}
	res.Content = code
	res.FlagSet = append(res.FlagSet, state.FLAG_LANG)
	languageSetFlag, err := h.flagManager.GetFlag("flag_language_set")
	if err != nil {
		logg.ErrorCtxf(ctx, "Error setting the languageSetFlag", "error", err)
		return res, err
	}
	res.FlagSet = append(res.FlagSet, languageSetFlag)

	return res, nil
}

// handles the account creation when no existing account is present for the session and stores associated data in the user data store.
func (h *MenuHandlers) createAccountNoExist(ctx context.Context, sessionId string, res *resource.Result) error {
	flag_account_created, _ := h.flagManager.GetFlag("flag_account_created")
	r, err := h.accountService.CreateAccount(ctx)
	if err != nil {
		return err
	}
	trackingId := r.TrackingId
	publicKey := r.PublicKey

	data := map[storedb.DataTyp]string{
		storedb.DATA_TRACKING_ID: trackingId,
		storedb.DATA_PUBLIC_KEY:  publicKey,
	}
	store := h.userdataStore
	for key, value := range data {
		err = store.WriteEntry(ctx, sessionId, key, []byte(value))
		if err != nil {
			return err
		}
	}
	publicKeyNormalized, err := hex.NormalizeHex(publicKey)
	if err != nil {
		return err
	}
	err = store.WriteEntry(ctx, publicKeyNormalized, storedb.DATA_PUBLIC_KEY_REVERSE, []byte(sessionId))
	if err != nil {
		return err
	}
	res.FlagSet = append(res.FlagSet, flag_account_created)
	return nil
}

// CreateAccount checks if any account exists on the JSON data file, and if not,
// creates an account on the API,
// sets the default values and flags.
func (h *MenuHandlers) CreateAccount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore
	_, err = store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		if db.IsNotFound(err) {
			logg.InfoCtxf(ctx, "Creating an account because it doesn't exist")
			err = h.createAccountNoExist(ctx, sessionId, &res)
			if err != nil {
				logg.ErrorCtxf(ctx, "failed on createAccountNoExist", "error", err)
				return res, err
			}
		}
	}

	return res, nil
}

// ResetValidPin resets the flag_valid_pin flag.
func (h *MenuHandlers) ResetValidPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_valid_pin, _ := h.flagManager.GetFlag("flag_valid_pin")
	res.FlagReset = append(res.FlagReset, flag_valid_pin)
	return res, nil
}

// CheckBlockedStatus resets the account blocked flag if the PIN attempts have been reset by an admin.
func (h *MenuHandlers) CheckBlockedStatus(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	flag_account_blocked, _ := h.flagManager.GetFlag("flag_account_blocked")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	currentWrongPinAttempts, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS)
	if err != nil {
		if !db.IsNotFound(err) {
			return res, nil
		}
	}

	pinAttemptsValue, _ := strconv.ParseUint(string(currentWrongPinAttempts), 0, 64)

	if pinAttemptsValue == 0 {
		res.FlagReset = append(res.FlagReset, flag_account_blocked)
		return res, nil
	}

	return res, nil
}

// ResetIncorrectPin resets the incorrect pin flag after a new PIN attempt.
func (h *MenuHandlers) ResetIncorrectPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	flag_incorrect_pin, _ := h.flagManager.GetFlag("flag_incorrect_pin")
	flag_account_blocked, _ := h.flagManager.GetFlag("flag_account_blocked")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_pin)

	currentWrongPinAttempts, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS)
	if err != nil {
		if !db.IsNotFound(err) {
			return res, err
		}
	}
	pinAttemptsValue, _ := strconv.ParseUint(string(currentWrongPinAttempts), 0, 64)
	remainingPINAttempts := pin.AllowedPINAttempts - uint8(pinAttemptsValue)
	if remainingPINAttempts == 0 {
		res.FlagSet = append(res.FlagSet, flag_account_blocked)
		return res, nil
	}
	if remainingPINAttempts < pin.AllowedPINAttempts {
		res.Content = strconv.Itoa(int(remainingPINAttempts))
	}

	return res, nil
}

// VerifyNewPin checks if a new PIN meets the required format criteria.
func (h *MenuHandlers) VerifyNewPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	res := resource.Result{}
	_, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_valid_pin, _ := h.flagManager.GetFlag("flag_valid_pin")
	if string(input) != "0" {
		pinInput := string(input)
		// Validate that the PIN is a 4-digit number.
		if pin.IsValidPIN(pinInput) {
			res.FlagSet = append(res.FlagSet, flag_valid_pin)
		} else {
			res.FlagReset = append(res.FlagReset, flag_valid_pin)
		}
	} else {
		res.FlagSet = append(res.FlagSet, flag_valid_pin)
	}

	return res, nil
}

// SaveTemporaryPin saves the valid PIN input to the DATA_TEMPORARY_VALUE,
// during the account creation process
// and during the change PIN process.
func (h *MenuHandlers) SaveTemporaryPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_incorrect_pin, _ := h.flagManager.GetFlag("flag_incorrect_pin")
	accountPIN := string(input)

	// Validate that the PIN is a 4-digit number.
	if !pin.IsValidPIN(accountPIN) {
		res.FlagSet = append(res.FlagSet, flag_incorrect_pin)
		return res, nil
	}
	res.FlagReset = append(res.FlagReset, flag_incorrect_pin)

	// Hash the PIN
	hashedPIN, err := pin.HashPIN(string(accountPIN))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to hash the PIN", "error", err)
		return res, err
	}

	store := h.userdataStore
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write temporaryAccountPIN entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", accountPIN, "error", err)
		return res, err
	}

	return res, nil
}

// SaveOthersTemporaryPin allows authorized users to set temporary PINs for blocked numbers.
func (h *MenuHandlers) SaveOthersTemporaryPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	temporaryPin := string(input)

	// Validate that the input is a 4-digit number.
	if !pin.IsValidPIN(temporaryPin) {
		return res, nil
	}

	// Retrieve the blocked number associated with this session
	blockedNumber, err := store.ReadEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read blockedNumber entry with", "key", storedb.DATA_BLOCKED_NUMBER, "error", err)
		return res, err
	}

	// Hash the temporary PIN
	hashedPIN, err := pin.HashPIN(string(temporaryPin))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to hash temporaryPin", "error", err)
		return res, err
	}

	// Save the hashed temporary PIN for that blocked number
	err = store.WriteEntry(ctx, string(blockedNumber), storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write hashed temporaryPin entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", temporaryPin, "error", err)
		return res, err
	}

	return res, nil
}

// CheckBlockedNumPinMisMatch checks if the provided PIN matches a temporary PIN stored for a blocked number.
func (h *MenuHandlers) CheckBlockedNumPinMisMatch(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	res := resource.Result{}
	flag_pin_mismatch, _ := h.flagManager.GetFlag("flag_pin_mismatch")
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	if string(input) == "0" {
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
		return res, nil
	}

	// Get blocked number from storage.
	store := h.userdataStore
	blockedNumber, err := store.ReadEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read blockedNumber entry with", "key", storedb.DATA_BLOCKED_NUMBER, "error", err)
		return res, err
	}
	// Get Hashed temporary PIN for the blocked number.
	hashedTemporaryPin, err := store.ReadEntry(ctx, string(blockedNumber), storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read hashedTemporaryPin entry with", "key", storedb.DATA_TEMPORARY_VALUE, "error", err)
		return res, err
	}
	if len(hashedTemporaryPin) == 0 {
		logg.ErrorCtxf(ctx, "hashedTemporaryPin is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("Data error encountered")
	}

	if pin.VerifyPIN(string(hashedTemporaryPin), string(input)) {
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
	} else {
		res.FlagSet = append(res.FlagSet, flag_pin_mismatch)
	}
	return res, nil
}

// ResetInvalidPIN resets the invalid PIN flag
func (h *MenuHandlers) ResetInvalidPIN(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_invalid_pin, _ := h.flagManager.GetFlag("flag_invalid_pin")
	res.FlagReset = append(res.FlagReset, flag_invalid_pin)
	return res, nil
}

// ConfirmPinChange validates user's new PIN. If input matches the temporary PIN, saves it as the new account PIN.
func (h *MenuHandlers) ConfirmPinChange(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_pin_mismatch, _ := h.flagManager.GetFlag("flag_pin_mismatch")

	if string(input) == "0" {
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
		return res, nil
	}

	store := h.userdataStore
	hashedTemporaryPin, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read hashedTemporaryPin entry with", "key", storedb.DATA_TEMPORARY_VALUE, "error", err)
		return res, err
	}
	if len(hashedTemporaryPin) == 0 {
		logg.ErrorCtxf(ctx, "hashedTemporaryPin is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("Data error encountered")
	}

	if pin.VerifyPIN(string(hashedTemporaryPin), string(input)) {
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
	} else {
		res.FlagSet = append(res.FlagSet, flag_pin_mismatch)
		return res, nil
	}

	// save the hashed PIN as the new account PIN
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN, []byte(hashedTemporaryPin))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write DATA_ACCOUNT_PIN entry with", "key", storedb.DATA_ACCOUNT_PIN, "hashedPIN value", hashedTemporaryPin, "error", err)
		return res, err
	}

	return res, nil
}

// ResetOthersPin handles the PIN reset process for other users' accounts by:
// 1. Retrieving the blocked phone number from the session
// 2. Fetching the hashed temporary PIN associated with that number
// 3. Updating the account PIN with the temporary PIN
func (h *MenuHandlers) ResetOthersPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	blockedPhonenumber, err := store.ReadEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read blockedPhonenumber entry with", "key", storedb.DATA_BLOCKED_NUMBER, "error", err)
		return res, err
	}
	hashedTemporaryPin, err := store.ReadEntry(ctx, string(blockedPhonenumber), storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read hashedTmporaryPin entry with", "key", storedb.DATA_TEMPORARY_VALUE, "error", err)
		return res, err
	}
	if len(hashedTemporaryPin) == 0 {
		logg.ErrorCtxf(ctx, "hashedTemporaryPin is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("Data error encountered")
	}

	err = store.WriteEntry(ctx, string(blockedPhonenumber), storedb.DATA_ACCOUNT_PIN, []byte(hashedTemporaryPin))
	if err != nil {
		return res, err
	}

	err = store.WriteEntry(ctx, string(blockedPhonenumber), storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(string("0")))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to reset incorrect PIN attempts", "key", storedb.DATA_INCORRECT_PIN_ATTEMPTS, "error", err)
		return res, err
	}

	return res, nil
}

// incrementIncorrectPINAttempts keeps track of the number of incorrect PIN attempts
func (h *MenuHandlers) incrementIncorrectPINAttempts(ctx context.Context, sessionId string) error {
	var pinAttemptsCount uint8
	store := h.userdataStore

	currentWrongPinAttempts, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS)
	if err != nil {
		if db.IsNotFound(err) {
			//First time Wrong PIN attempt: initialize with a count of 1
			pinAttemptsCount = 1
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(strconv.Itoa(int(pinAttemptsCount))))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write incorrect PIN attempts ", "key", storedb.DATA_INCORRECT_PIN_ATTEMPTS, "value", currentWrongPinAttempts, "error", err)
				return err
			}
			return nil
		}
	}
	pinAttemptsValue, _ := strconv.ParseUint(string(currentWrongPinAttempts), 0, 64)
	pinAttemptsCount = uint8(pinAttemptsValue) + 1

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(strconv.Itoa(int(pinAttemptsCount))))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write incorrect PIN attempts ", "key", storedb.DATA_INCORRECT_PIN_ATTEMPTS, "value", pinAttemptsCount, "error", err)
		return err
	}
	return nil
}

// resetIncorrectPINAttempts resets the number of incorrect PIN attempts after a correct PIN entry
func (h *MenuHandlers) resetIncorrectPINAttempts(ctx context.Context, sessionId string) error {
	store := h.userdataStore
	currentWrongPinAttempts, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS)
	if err != nil {
		if db.IsNotFound(err) {
			return nil
		}
		return err
	}
	currentWrongPinAttemptsCount, _ := strconv.ParseUint(string(currentWrongPinAttempts), 0, 64)
	if currentWrongPinAttemptsCount <= uint64(pin.AllowedPINAttempts) {
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(string("0")))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to reset incorrect PIN attempts ", "key", storedb.DATA_INCORRECT_PIN_ATTEMPTS, "value", pin.AllowedPINAttempts, "error", err)
			return err
		}
	}
	return nil
}

// ResetUnregisteredNumber clears the unregistered number flag in the system,
// indicating that a number's registration status should no longer be marked as unregistered.
func (h *MenuHandlers) ResetUnregisteredNumber(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_unregistered_number, _ := h.flagManager.GetFlag("flag_unregistered_number")
	res.FlagReset = append(res.FlagReset, flag_unregistered_number)
	return res, nil
}

// ValidateBlockedNumber performs validation of phone numbers, specifically for blocked numbers in the system.
// It checks phone number format and verifies registration status.
func (h *MenuHandlers) ValidateBlockedNumber(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	flag_unregistered_number, _ := h.flagManager.GetFlag("flag_unregistered_number")
	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	if string(input) == "0" {
		res.FlagReset = append(res.FlagReset, flag_unregistered_number)
		return res, nil
	}

	blockedNumber := string(input)
	formattedNumber, err := phone.FormatPhoneNumber(blockedNumber)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_unregistered_number)
		logg.ErrorCtxf(ctx, "Failed to format the phone number: %s", blockedNumber, "error", err)
		return res, nil
	}

	_, err = store.ReadEntry(ctx, formattedNumber, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		if db.IsNotFound(err) {
			logg.InfoCtxf(ctx, "Invalid or unregistered number")
			res.FlagSet = append(res.FlagSet, flag_unregistered_number)
			return res, nil
		} else {
			logg.ErrorCtxf(ctx, "Error on ValidateBlockedNumber", "error", err)
			return res, err
		}
	}
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER, []byte(formattedNumber))
	if err != nil {
		return res, nil
	}
	return res, nil
}

// VerifyCreatePin checks whether the confirmation PIN is similar to the temporary PIN
// If similar, it sets the USERFLAG_PIN_SET flag and writes the account PIN allowing the user
// to access the main menu.
func (h *MenuHandlers) VerifyCreatePin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_valid_pin, _ := h.flagManager.GetFlag("flag_valid_pin")
	flag_pin_mismatch, _ := h.flagManager.GetFlag("flag_pin_mismatch")
	flag_pin_set, _ := h.flagManager.GetFlag("flag_pin_set")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore
	hashedTemporaryPin, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read hashedTemporaryPin entry with", "key", storedb.DATA_TEMPORARY_VALUE, "error", err)
		return res, err
	}
	if len(hashedTemporaryPin) == 0 {
		logg.ErrorCtxf(ctx, "hashedTemporaryPin is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("Data error encountered")
	}

	if pin.VerifyPIN(string(hashedTemporaryPin), string(input)) {
		res.FlagSet = []uint32{flag_valid_pin}
		res.FlagReset = []uint32{flag_pin_mismatch}
		res.FlagSet = append(res.FlagSet, flag_pin_set)
	} else {
		res.FlagSet = []uint32{flag_pin_mismatch}
		return res, nil
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN, []byte(hashedTemporaryPin))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write DATA_ACCOUNT_PIN entry with", "key", storedb.DATA_ACCOUNT_PIN, "value", hashedTemporaryPin, "error", err)
		return res, err
	}

	return res, nil
}

// SaveFirstname updates the first name in the gdbm with the provided input.
func (h *MenuHandlers) SaveFirstname(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	firstName := string(input)
	store := h.userdataStore
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_firstname_set, _ := h.flagManager.GetFlag("flag_firstname_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	firstNameSet := h.st.MatchFlag(flag_firstname_set, true)
	if allowUpdate {
		temporaryFirstName, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryFirstName) == 0 {
			logg.ErrorCtxf(ctx, "temporaryFirstName is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_FIRST_NAME, []byte(temporaryFirstName))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write firstName entry with", "key", storedb.DATA_FIRST_NAME, "value", temporaryFirstName, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_firstname_set)
	} else {
		if firstNameSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(firstName))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryFirstName entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", firstName, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(0, firstName)
		}
	}

	return res, nil
}

// SaveFamilyname updates the family name in the gdbm with the provided input.
func (h *MenuHandlers) SaveFamilyname(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	store := h.userdataStore
	familyName := string(input)

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_familyname_set, _ := h.flagManager.GetFlag("flag_familyname_set")
	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	familyNameSet := h.st.MatchFlag(flag_familyname_set, true)

	if allowUpdate {
		temporaryFamilyName, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryFamilyName) == 0 {
			logg.ErrorCtxf(ctx, "temporaryFamilyName is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME, []byte(temporaryFamilyName))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write familyName entry with", "key", storedb.DATA_FAMILY_NAME, "value", temporaryFamilyName, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_familyname_set)
	} else {
		if familyNameSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(familyName))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryFamilyName entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", familyName, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(1, familyName)
		}
	}

	return res, nil
}

// VerifyYob verifies the length of the given input.
func (h *MenuHandlers) VerifyYob(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	flag_incorrect_date_format, _ := h.flagManager.GetFlag("flag_incorrect_date_format")
	date := string(input)
	_, err = strconv.Atoi(date)
	if err != nil {
		// If conversion fails, input is not numeric
		res.FlagSet = append(res.FlagSet, flag_incorrect_date_format)
		return res, nil
	}

	if person.IsValidYOb(date) {
		res.FlagReset = append(res.FlagReset, flag_incorrect_date_format)
	} else {
		res.FlagSet = append(res.FlagSet, flag_incorrect_date_format)
	}
	return res, nil
}

// ResetIncorrectYob resets the incorrect date format flag after a new attempt.
func (h *MenuHandlers) ResetIncorrectYob(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_incorrect_date_format, _ := h.flagManager.GetFlag("flag_incorrect_date_format")
	res.FlagReset = append(res.FlagReset, flag_incorrect_date_format)
	return res, nil
}

// SaveYOB updates the Year of Birth(YOB) in the gdbm with the provided input.
func (h *MenuHandlers) SaveYob(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	yob := string(input)
	store := h.userdataStore
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_yob_set, _ := h.flagManager.GetFlag("flag_yob_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	yobSet := h.st.MatchFlag(flag_yob_set, true)

	if allowUpdate {
		temporaryYob, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryYob) == 0 {
			logg.ErrorCtxf(ctx, "temporaryYob is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_YOB, []byte(temporaryYob))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write yob entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", temporaryYob, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_yob_set)
	} else {
		if yobSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(yob))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryYob entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", yob, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(3, yob)
		}
	}

	return res, nil
}

// SaveLocation updates the location in the gdbm with the provided input.
func (h *MenuHandlers) SaveLocation(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	location := string(input)
	store := h.userdataStore

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_location_set, _ := h.flagManager.GetFlag("flag_location_set")
	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	locationSet := h.st.MatchFlag(flag_location_set, true)

	if allowUpdate {
		temporaryLocation, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryLocation) == 0 {
			logg.ErrorCtxf(ctx, "temporaryLocation is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_LOCATION, []byte(temporaryLocation))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write location entry with", "key", storedb.DATA_LOCATION, "value", temporaryLocation, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_location_set)
	} else {
		if locationSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(location))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryLocation entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", location, "error", err)
				return res, err
			}
			res.FlagSet = append(res.FlagSet, flag_location_set)
		} else {
			h.profile.InsertOrShift(4, location)
		}
	}

	return res, nil
}

// SaveGender updates the gender in the gdbm with the provided input.
func (h *MenuHandlers) SaveGender(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	symbol, _ := h.st.Where()
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	gender := strings.Split(symbol, "_")[1]
	store := h.userdataStore
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_gender_set, _ := h.flagManager.GetFlag("flag_gender_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	genderSet := h.st.MatchFlag(flag_gender_set, true)

	if allowUpdate {
		temporaryGender, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryGender) == 0 {
			logg.ErrorCtxf(ctx, "temporaryGender is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_GENDER, []byte(temporaryGender))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write gender entry with", "key", storedb.DATA_GENDER, "value", gender, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_gender_set)
	} else {
		if genderSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(gender))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryGender entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", gender, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(2, gender)
		}
	}

	return res, nil
}

// SaveOfferings updates the offerings(goods and services provided by the user) in the gdbm with the provided input.
func (h *MenuHandlers) SaveOfferings(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	offerings := string(input)
	store := h.userdataStore

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_offerings_set, _ := h.flagManager.GetFlag("flag_offerings_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	offeringsSet := h.st.MatchFlag(flag_offerings_set, true)

	if allowUpdate {
		temporaryOfferings, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryOfferings) == 0 {
			logg.ErrorCtxf(ctx, "temporaryOfferings is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_OFFERINGS, []byte(temporaryOfferings))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write offerings entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", offerings, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_offerings_set)
	} else {
		if offeringsSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(offerings))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryOfferings entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", offerings, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(5, offerings)
		}
	}

	return res, nil
}

// ResetAllowUpdate resets the allowupdate flag that allows a user to update  profile data.
func (h *MenuHandlers) ResetAllowUpdate(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	res.FlagReset = append(res.FlagReset, flag_allow_update)
	return res, nil
}

// GetCurrentProfileInfo retrieves specific profile fields based on the current state of the USSD session.
// Uses flag management system to track profile field status and handle menu navigation.
func (h *MenuHandlers) GetCurrentProfileInfo(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var profileInfo []byte
	var defaultValue string
	var err error

	flag_firstname_set, _ := h.flagManager.GetFlag("flag_firstname_set")
	flag_familyname_set, _ := h.flagManager.GetFlag("flag_familyname_set")
	flag_yob_set, _ := h.flagManager.GetFlag("flag_yob_set")
	flag_gender_set, _ := h.flagManager.GetFlag("flag_gender_set")
	flag_location_set, _ := h.flagManager.GetFlag("flag_location_set")
	flag_offerings_set, _ := h.flagManager.GetFlag("flag_offerings_set")
	flag_back_set, _ := h.flagManager.GetFlag("flag_back_set")

	res.FlagReset = append(res.FlagReset, flag_back_set)

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	language, ok := ctx.Value("Language").(lang.Language)
	if !ok {
		return res, fmt.Errorf("value for 'Language' is not of type lang.Language")
	}
	code := language.Code
	if code == "swa" {
		defaultValue = "Haipo"
	} else {
		defaultValue = "Not Provided"
	}

	sm, _ := h.st.Where()
	parts := strings.SplitN(sm, "_", 2)
	filename := parts[1]
	dbKeyStr := "DATA_" + strings.ToUpper(filename)
	dbKey, err := storedb.StringToDataTyp(dbKeyStr)

	if err != nil {
		return res, err
	}
	store := h.userdataStore

	switch dbKey {
	case storedb.DATA_FIRST_NAME:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read first name entry with", "key", "error", storedb.DATA_FIRST_NAME, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_firstname_set)
		res.Content = string(profileInfo)
	case storedb.DATA_FAMILY_NAME:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read family name entry with", "key", "error", storedb.DATA_FAMILY_NAME, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_familyname_set)
		res.Content = string(profileInfo)

	case storedb.DATA_GENDER:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_GENDER)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read gender entry with", "key", "error", storedb.DATA_GENDER, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_gender_set)
		res.Content = string(profileInfo)
	case storedb.DATA_YOB:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_YOB)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read year of birth(yob) entry with", "key", "error", storedb.DATA_YOB, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_yob_set)
		res.Content = string(profileInfo)
	case storedb.DATA_LOCATION:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_LOCATION)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read location entry with", "key", "error", storedb.DATA_LOCATION, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_location_set)
		res.Content = string(profileInfo)
	case storedb.DATA_OFFERINGS:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_OFFERINGS)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read offerings entry with", "key", "error", storedb.DATA_OFFERINGS, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_offerings_set)
		res.Content = string(profileInfo)
	case storedb.DATA_ACCOUNT_ALIAS:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read account alias entry with", "key", "error", storedb.DATA_ACCOUNT_ALIAS, err)
			return res, err
		}
		res.Content = string(profileInfo)
	default:
		break
	}

	return res, nil
}

// GetProfileInfo provides a comprehensive view of a user's profile.
func (h *MenuHandlers) GetProfileInfo(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var defaultValue string
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	language, ok := ctx.Value("Language").(lang.Language)
	if !ok {
		return res, fmt.Errorf("value for 'Language' is not of type lang.Language")
	}
	code := language.Code
	if code == "swa" {
		defaultValue = "Haipo"
	} else {
		defaultValue = "Not Provided"
	}

	// Helper function to handle nil byte slices and convert them to string
	getEntryOrDefault := func(entry []byte, err error) string {
		if err != nil || entry == nil {
			return defaultValue
		}
		return string(entry)
	}
	store := h.userdataStore
	// Retrieve user data as strings with fallback to defaultValue
	firstName := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME))
	familyName := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME))
	yob := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_YOB))
	gender := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_GENDER))
	location := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_LOCATION))
	offerings := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_OFFERINGS))
	alias := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS))

	if alias != defaultValue {
		alias = strings.Split(alias, ".")[0]
	}

	// Construct the full name
	name := person.ConstructName(firstName, familyName, defaultValue)

	// Calculate age from year of birth
	age := defaultValue
	if yob != defaultValue {
		if yobInt, err := strconv.Atoi(yob); err == nil {
			age = strconv.Itoa(person.CalculateAgeWithYOB(yobInt))
		} else {
			return res, fmt.Errorf("invalid year of birth: %v", err)
		}
	}
	switch language.Code {
	case "eng":
		res.Content = fmt.Sprintf(
			"Name: %s\nGender: %s\nAge: %s\nLocation: %s\nYou provide: %s\nYour alias: %s\n",
			name, gender, age, location, offerings, alias,
		)
	case "swa":
		res.Content = fmt.Sprintf(
			"Jina: %s\nJinsia: %s\nUmri: %s\nEneo: %s\nUnauza: %s\nLakabu yako: %s\n",
			name, gender, age, location, offerings, alias,
		)
	default:
		res.Content = fmt.Sprintf(
			"Name: %s\nGender: %s\nAge: %s\nLocation: %s\nYou provide: %s\nYour alias: %s\n",
			name, gender, age, location, offerings, alias,
		)
	}

	return res, nil
}

// handles bulk updates of profile information.
func (h *MenuHandlers) insertProfileItems(ctx context.Context, sessionId string, res *resource.Result) error {
	var err error
	userStore := h.userdataStore
	profileFlagNames := []string{
		"flag_firstname_set",
		"flag_familyname_set",
		"flag_yob_set",
		"flag_gender_set",
		"flag_location_set",
		"flag_offerings_set",
	}
	profileDataKeys := []storedb.DataTyp{
		storedb.DATA_FIRST_NAME,
		storedb.DATA_FAMILY_NAME,
		storedb.DATA_GENDER,
		storedb.DATA_YOB,
		storedb.DATA_LOCATION,
		storedb.DATA_OFFERINGS,
	}
	for index, profileItem := range h.profile.ProfileItems {
		// Ensure the profileItem is not "0"(is set)
		if profileItem != "0" {
			flag, _ := h.flagManager.GetFlag(profileFlagNames[index])
			isProfileItemSet := h.st.MatchFlag(flag, true)
			if !isProfileItemSet {
				err = userStore.WriteEntry(ctx, sessionId, profileDataKeys[index], []byte(profileItem))
				if err != nil {
					logg.ErrorCtxf(ctx, "failed to write profile entry with", "key", profileDataKeys[index], "value", profileItem, "error", err)
					return err
				}
				res.FlagSet = append(res.FlagSet, flag)
			}
		}
	}
	return nil
}

// UpdateAllProfileItems  is used to persist all the  new profile information and setup  the required profile flags.
func (h *MenuHandlers) UpdateAllProfileItems(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_alias_set, _ := h.flagManager.GetFlag("flag_alias_set")
	aliasSet := h.st.MatchFlag(flag_alias_set, true)

	err := h.insertProfileItems(ctx, sessionId, &res)
	if err != nil {
		return res, err
	}
	//Only request an alias if it has not been set yet:
	if !aliasSet {
		err = h.constructAccountAlias(ctx)
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// ResetAccountAuthorized resets the account authorization flag after a successful PIN entry.
func (h *MenuHandlers) ResetAccountAuthorized(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")
	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

// CheckIdentifier retrieves the PublicKey from the JSON data file.
func (h *MenuHandlers) CheckIdentifier(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore
	publicKey, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)

	res.Content = string(publicKey)

	return res, nil
}

// Authorize attempts to unlock the next sequential nodes by verifying the provided PIN against the already set PIN.
// It sets the required flags that control the flow.
func (h *MenuHandlers) Authorize(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_incorrect_pin, _ := h.flagManager.GetFlag("flag_incorrect_pin")
	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_invalid_pin, _ := h.flagManager.GetFlag("flag_invalid_pin")

	store := h.userdataStore
	AccountPin, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read AccountPin entry with", "key", storedb.DATA_ACCOUNT_PIN, "error", err)
		return res, err
	}
	str := string(input)
	_, err = strconv.Atoi(str)
	if len(input) == 4 && err == nil {
		if pin.VerifyPIN(string(AccountPin), string(input)) {
			if h.st.MatchFlag(flag_account_authorized, false) {
				res.FlagReset = append(res.FlagReset, flag_incorrect_pin)
				res.FlagSet = append(res.FlagSet, flag_allow_update, flag_account_authorized)
				err := h.resetIncorrectPINAttempts(ctx, sessionId)
				if err != nil {
					return res, err
				}
			} else {
				res.FlagSet = append(res.FlagSet, flag_allow_update)
				res.FlagReset = append(res.FlagReset, flag_account_authorized)
				err := h.resetIncorrectPINAttempts(ctx, sessionId)
				if err != nil {
					return res, err
				}
			}
		} else {
			err = h.incrementIncorrectPINAttempts(ctx, sessionId)
			if err != nil {
				return res, err
			}
			res.FlagSet = append(res.FlagSet, flag_incorrect_pin)
			res.FlagReset = append(res.FlagReset, flag_account_authorized)
			return res, nil
		}
	} else {
		if string(input) != "0" {
			res.FlagSet = append(res.FlagSet, flag_invalid_pin)
		}
		return res, nil
	}
	return res, nil
}

// Setback sets the flag_back_set flag when the navigation is back.
func (h *MenuHandlers) SetBack(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_back_set, _ := h.flagManager.GetFlag("flag_back_set")
	//TODO:
	//Add check if the navigation is lateral nav instead of checking the input.
	if string(input) == "0" {
		res.FlagSet = append(res.FlagSet, flag_back_set)
	} else {
		res.FlagReset = append(res.FlagReset, flag_back_set)
	}
	return res, nil
}

// CheckAccountStatus queries the API using the TrackingId and sets flags
// based on the account status.
func (h *MenuHandlers) CheckAccountStatus(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_account_success, _ := h.flagManager.GetFlag("flag_account_success")
	flag_account_pending, _ := h.flagManager.GetFlag("flag_account_pending")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	store := h.userdataStore
	publicKey, err := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	r, err := h.accountService.TrackAccountStatus(ctx, string(publicKey))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on TrackAccountStatus", "error", err)
		return res, err
	}

	res.FlagReset = append(res.FlagReset, flag_api_error)

	if r.Active {
		res.FlagSet = append(res.FlagSet, flag_account_success)
		res.FlagReset = append(res.FlagReset, flag_account_pending)
	} else {
		res.FlagReset = append(res.FlagReset, flag_account_success)
		res.FlagSet = append(res.FlagSet, flag_account_pending)
	}

	return res, nil
}

// Quit displays the Thank you message and exits the menu.
func (h *MenuHandlers) Quit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	res.Content = l.Get("Thank you for using Sarafu. Goodbye!")
	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

// QuitWithHelp displays helpline information then exits the menu.
func (h *MenuHandlers) QuitWithHelp(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	res.Content = l.Get("For more help, please call: 0757628885")
	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

// ShowBlockedAccount displays a message after an account has been blocked and how to reach support.
func (h *MenuHandlers) ShowBlockedAccount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")
	res.Content = l.Get("Your account has been locked. For help on how to unblock your account, contact support at: 0757628885")
	return res, nil
}

// loadUserContent loads the main user content in the main menu: the alias,balance associated with active voucher
func loadUserContent(ctx context.Context, activeSym string, balance string, alias string) (string, error) {
	var content string

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	balFloat, err := strconv.ParseFloat(balance, 64)
	if err != nil {
		//Only exclude ErrSyntax error to avoid returning an error if the active bal is not available yet
		if !errors.Is(err, strconv.ErrSyntax) {
			logg.ErrorCtxf(ctx, "failed to parse activeBal as float", "value", balance, "error", err)
			return "", err
		}
		balFloat = 0.00
	}
	// Format to 2 decimal places
	balStr := fmt.Sprintf("%.2f %s", balFloat, activeSym)
	if alias != "" {
		content = l.Get("%s balance: %s\n", alias, balStr)
	} else {
		content = l.Get("balance: %s\n", balStr)
	}
	return content, nil
}

// CheckBalance retrieves the balance of the active voucher and sets
// the balance as the result content.
func (h *MenuHandlers) CheckBalance(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var (
		res     resource.Result
		err     error
		alias   string
		content string
	)

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	store := h.userdataStore

	accAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS)
	if err != nil {
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read account alias entry with", "key", storedb.DATA_ACCOUNT_ALIAS, "error", err)
			return res, err
		}
	} else {
		alias = strings.Split(string(accAlias), ".")[0]
	}

	// get the active sym and active balance
	activeSym, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.InfoCtxf(ctx, "could not find the activeSym in checkBalance:", "err", err)
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
			return res, err
		}
	}

	logg.InfoCtxf(ctx, "The active data in CheckBalance:", "activeSym", string(activeSym))

	activeBal, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
			return res, err
		}
	}

	content, err = loadUserContent(ctx, string(activeSym), string(activeBal), alias)
	if err != nil {
		return res, err
	}
	res.Content = content

	return res, nil
}

// FetchCommunityBalance retrieves and displays the balance for community accounts in user's preferred language.
func (h *MenuHandlers) FetchCommunityBalance(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	// retrieve the language code from the context
	code := codeFromCtx(ctx)
	// Initialize the localization system with the appropriate translation directory
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")
	//TODO:
	//Check if the address is a community account,if then,get the actual balance
	res.Content = l.Get("Community Balance: 0.00")
	return res, nil
}

// ValidateRecipient validates that the given input is valid.
//
// TODO: split up functino
func (h *MenuHandlers) ValidateRecipient(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var AliasAddressResult string
	var AliasAddress *models.AliasAddress
	store := h.userdataStore

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_invalid_recipient, _ := h.flagManager.GetFlag("flag_invalid_recipient")
	flag_invalid_recipient_with_invite, _ := h.flagManager.GetFlag("flag_invalid_recipient_with_invite")

	recipient := string(input)

	if recipient != "0" {
		recipientType, err := identity.CheckRecipient(recipient)
		if err != nil {
			// Invalid recipient format (not a phone number, address, or valid alias format)
			res.FlagSet = append(res.FlagSet, flag_invalid_recipient)
			res.Content = recipient

			return res, nil
		}

		// save the recipient as the temporaryRecipient
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(recipient))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write temporaryRecipient entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", recipient, "error", err)
			return res, err
		}

		switch recipientType {
		case "phone number":
			// format the phone number
			formattedNumber, err := phone.FormatPhoneNumber(recipient)
			if err != nil {
				logg.ErrorCtxf(ctx, "Failed to format the phone number: %s", recipient, "error", err)
				return res, err
			}

			// Check if the phone number is registered
			publicKey, err := store.ReadEntry(ctx, formattedNumber, storedb.DATA_PUBLIC_KEY)
			if err != nil {
				if db.IsNotFound(err) {
					logg.InfoCtxf(ctx, "Unregistered phone number: %s", recipient)
					res.FlagSet = append(res.FlagSet, flag_invalid_recipient_with_invite)
					res.Content = recipient
					return res, nil
				}

				logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
				return res, err
			}

			// Save the publicKey as the recipient
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, publicKey)
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write recipient entry with", "key", storedb.DATA_RECIPIENT, "value", string(publicKey), "error", err)
				return res, err
			}

		case "address":
			// Save the valid Ethereum address as the recipient
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(recipient))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write recipient entry with", "key", storedb.DATA_RECIPIENT, "value", recipient, "error", err)
				return res, err
			}

		case "alias":
			if strings.Contains(recipient, ".") {
				AliasAddress, err = h.accountService.CheckAliasAddress(ctx, recipient)
				if err == nil {
					AliasAddressResult = AliasAddress.Address
				} else {
					logg.ErrorCtxf(ctx, "failed to resolve alias", "alias", recipient, "error_alias_check", err)
				}
			} else {
				//Perform a search for each search domain,break on first match
				for _, domain := range config.SearchDomains() {
					fqdn := fmt.Sprintf("%s.%s", recipient, domain)
					logg.InfoCtxf(ctx, "Resolving with fqdn alias", "alias", fqdn)
					AliasAddress, err = h.accountService.CheckAliasAddress(ctx, fqdn)
					if err == nil {
						AliasAddressResult = AliasAddress.Address
						continue
					} else {
						logg.ErrorCtxf(ctx, "failed to resolve alias", "alias", recipient, "error_alias_check", err)
					}
				}
			}
			if AliasAddressResult == "" {
				res.Content = recipient
				res.FlagSet = append(res.FlagSet, flag_invalid_recipient)
				return res, nil
			} else {
				err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(AliasAddressResult))
				if err != nil {
					logg.ErrorCtxf(ctx, "failed to write recipient entry with", "key", storedb.DATA_RECIPIENT, "value", AliasAddressResult, "error", err)
					return res, err
				}
			}
		}
	}

	return res, nil
}

// TransactionReset resets the previous transaction data (Recipient and Amount)
// as well as the invalid flags.
func (h *MenuHandlers) TransactionReset(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_invalid_recipient, _ := h.flagManager.GetFlag("flag_invalid_recipient")
	flag_invalid_recipient_with_invite, _ := h.flagManager.GetFlag("flag_invalid_recipient_with_invite")
	store := h.userdataStore
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(""))
	if err != nil {
		return res, nil
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(""))
	if err != nil {
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_invalid_recipient, flag_invalid_recipient_with_invite)

	return res, nil
}

// InviteValidRecipient sends an invitation to the valid phone number.
func (h *MenuHandlers) InviteValidRecipient(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	recipient, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if len(recipient) == 0 {
		logg.ErrorCtxf(ctx, "recipient is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("Data error encountered")
	}

	// TODO
	// send an invitation SMS
	// if successful
	// res.Content = l.Get("Your invitation to %s to join Sarafu Network has been sent.",  string(recipient))

	res.Content = l.Get("Your invite request for %s to Sarafu Network failed. Please try again later.", string(recipient))
	return res, nil
}

// ResetTransactionAmount resets the transaction amount and invalid flag.
func (h *MenuHandlers) ResetTransactionAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	store := h.userdataStore
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(""))
	if err != nil {
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_invalid_amount)

	return res, nil
}

// MaxAmount gets the current balance from the API and sets it as
// the result content.
func (h *MenuHandlers) MaxAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore

	activeBal, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
		return res, err
	}

	res.Content = string(activeBal)

	return res, nil
}

// ValidateAmount ensures that the given input is a valid amount and that
// it is not more than the current balance.
func (h *MenuHandlers) ValidateAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	store := h.userdataStore

	var balanceValue float64

	// retrieve the active balance
	activeBal, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
		return res, err
	}
	balanceValue, err = strconv.ParseFloat(string(activeBal), 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the activeBal to a float", "error", err)
		return res, err
	}

	// Extract numeric part from the input amount
	amountStr := strings.TrimSpace(string(input))
	inputAmount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = amountStr
		return res, nil
	}

	if inputAmount > balanceValue {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = amountStr
		return res, nil
	}

	// Format the amount with 2 decimal places before saving
	formattedAmount := fmt.Sprintf("%.2f", inputAmount)
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(formattedAmount))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write amount entry with", "key", storedb.DATA_AMOUNT, "value", formattedAmount, "error", err)
		return res, err
	}

	res.Content = formattedAmount
	return res, nil
}

// GetRecipient returns the transaction recipient phone number from the gdbm.
func (h *MenuHandlers) GetRecipient(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore
	recipient, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if len(recipient) == 0 {
		logg.ErrorCtxf(ctx, "recipient is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("Data error encountered")
	}

	res.Content = string(recipient)

	return res, nil
}

// RetrieveBlockedNumber gets the current number during the pin reset for other's is in progress.
func (h *MenuHandlers) RetrieveBlockedNumber(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore
	blockedNumber, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER)

	res.Content = string(blockedNumber)

	return res, nil
}

// GetSender returns the sessionId (phoneNumber).
func (h *MenuHandlers) GetSender(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	res.Content = sessionId

	return res, nil
}

// GetAmount retrieves the amount from teh Gdbm Db.
func (h *MenuHandlers) GetAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore

	// retrieve the active symbol
	activeSym, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
		return res, err
	}

	amount, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)

	res.Content = fmt.Sprintf("%s %s", string(amount), string(activeSym))

	return res, nil
}

// InitiateTransaction calls the TokenTransfer and returns a confirmation based on the result.
func (h *MenuHandlers) InitiateTransaction(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	data, err := store.ReadTransactionData(ctx, h.userdataStore, sessionId)
	if err != nil {
		return res, err
	}

	finalAmountStr, err := store.ParseAndScaleAmount(data.Amount, data.ActiveDecimal)
	if err != nil {
		return res, err
	}

	// Call TokenTransfer
	r, err := h.accountService.TokenTransfer(ctx, finalAmountStr, data.PublicKey, data.Recipient, data.ActiveAddress)
	if err != nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on TokenTransfer", "error", err)
		return res, nil
	}

	trackingId := r.TrackingId
	logg.InfoCtxf(ctx, "TokenTransfer", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. %s will receive %s %s from %s.",
		data.TemporaryValue,
		data.Amount,
		data.ActiveSym,
		sessionId,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

// SetDefaultVoucher retrieves the current vouchers
// and sets the first as the default voucher, if no active voucher is set.
func (h *MenuHandlers) SetDefaultVoucher(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	userStore := h.userdataStore

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_no_active_voucher, _ := h.flagManager.GetFlag("flag_no_active_voucher")

	// check if the user has an active sym
	activeSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)

	if err != nil {
		logg.InfoCtxf(ctx, "Checking the data as no activeSym", "DATA_ACTIVE_SYM", storedb.DATA_ACTIVE_SYM)

		if db.IsNotFound(err) {
			publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
				return res, err
			}

			// Fetch vouchers from the API using the public key
			vouchersResp, err := h.accountService.FetchVouchers(ctx, string(publicKey))
			if err != nil {
				res.FlagSet = append(res.FlagSet, flag_no_active_voucher)
				return res, nil
			}

			logg.InfoCtxf(ctx, "fetched user vouchers in SetDefaultVoucher", "public_key", string(publicKey), "vouchers", vouchersResp)

			// Return if there is no voucher
			if len(vouchersResp) == 0 {
				res.FlagSet = append(res.FlagSet, flag_no_active_voucher)
				return res, nil
			}

			// Use only the first voucher
			firstVoucher := vouchersResp[0]
			defaultSym := firstVoucher.TokenSymbol
			defaultBal := firstVoucher.Balance
			defaultDec := firstVoucher.TokenDecimals
			defaultAddr := firstVoucher.ContractAddress

			// Scale down the balance
			scaledBalance := store.ScaleDownBalance(defaultBal, defaultDec)

			logg.InfoCtxf(ctx, "firstVoucher data", "defaultSym", defaultSym, "defaultBal", defaultBal, "defaultDec", defaultDec, "defaultAddr", defaultAddr)

			// TODO: implement atomic transaction
			// set the active symbol
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM, []byte(defaultSym))
			if err != nil {
				logg.InfoCtxf(ctx, "got an error in writing DATA_ACTIVE_SYM", "defaultSym", defaultSym)

				logg.ErrorCtxf(ctx, "failed to write defaultSym entry with", "key", storedb.DATA_ACTIVE_SYM, "value", defaultSym, "error", err)
				return res, err
			}
			// set the active balance
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL, []byte(scaledBalance))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write defaultBal entry with", "key", storedb.DATA_ACTIVE_BAL, "value", scaledBalance, "error", err)
				return res, err
			}
			// set the active decimals
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_DECIMAL, []byte(defaultDec))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write defaultDec entry with", "key", storedb.DATA_ACTIVE_DECIMAL, "value", defaultDec, "error", err)
				return res, err
			}
			// set the active contract address
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS, []byte(defaultAddr))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write defaultAddr entry with", "key", storedb.DATA_ACTIVE_ADDRESS, "value", defaultAddr, "error", err)
				return res, err
			}

			return res, nil
		}

		logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
		return res, err
	}

	logg.InfoCtxf(ctx, "The activeSym in SetDefaultVoucher:", "activeSym", string(activeSym))

	res.FlagReset = append(res.FlagReset, flag_no_active_voucher)

	return res, nil
}

// CheckVouchers retrieves the token holdings from the API using the "PublicKey" and stores
// them to gdbm.
func (h *MenuHandlers) CheckVouchers(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Fetch vouchers from the API using the public key
	vouchersResp, err := h.accountService.FetchVouchers(ctx, string(publicKey))
	if err != nil {
		return res, nil
	}

	logg.InfoCtxf(ctx, "fetched user vouchers", "public_key", string(publicKey), "vouchers", vouchersResp)

	// check the current active sym and update the data
	activeSym, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if activeSym != nil {
		activeSymStr := string(activeSym)

		// Find the matching voucher data
		var activeData *dataserviceapi.TokenHoldings
		for _, voucher := range vouchersResp {
			if voucher.TokenSymbol == activeSymStr {
				activeData = &voucher
				break
			}
		}

		if activeData == nil {
			logg.ErrorCtxf(ctx, "activeSym not found in vouchers", "activeSym", activeSymStr)
			return res, fmt.Errorf("activeSym %s not found in vouchers", activeSymStr)
		}

		// Scale down the balance
		scaledBalance := store.ScaleDownBalance(activeData.Balance, activeData.TokenDecimals)

		// Update the balance field with the scaled value
		activeData.Balance = scaledBalance

		// Pass the matching voucher data to UpdateVoucherData
		if err := store.UpdateVoucherData(ctx, h.userdataStore, sessionId, activeData); err != nil {
			logg.ErrorCtxf(ctx, "failed on UpdateVoucherData", "error", err)
			return res, err
		}
	}

	activeBal, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	activeAddr, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)

	logg.InfoCtxf(ctx, "The active data in CheckVouchers:", "activeSym", string(activeSym), string(activeBal), string(activeAddr))

	data := store.ProcessVouchers(vouchersResp)

	logg.InfoCtxf(ctx, "The data in CheckVouchers:", "data", data)

	// Store all voucher data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_VOUCHER_SYMBOLS:   data.Symbols,
		storedb.DATA_VOUCHER_BALANCES:  data.Balances,
		storedb.DATA_VOUCHER_DECIMALS:  data.Decimals,
		storedb.DATA_VOUCHER_ADDRESSES: data.Addresses,
	}

	for key, value := range dataMap {
		if err := h.prefixDb.Put(ctx, []byte(storedb.ToBytes(key)), []byte(value)); err != nil {
			return res, nil
		}
	}

	return res, nil
}

// GetVoucherList fetches the list of vouchers and formats them.
func (h *MenuHandlers) GetVoucherList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	// Read vouchers from the store
	voucherData, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_VOUCHER_SYMBOLS))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the voucherData from prefixDb", "error", err)
		return res, err
	}

	formattedData := h.ReplaceSeparatorFunc(string(voucherData))

	res.Content = string(formattedData)

	return res, nil
}

// ViewVoucher retrieves the token holding and balance from the subprefixDB
// and displays it to the user for them to select it.
func (h *MenuHandlers) ViewVoucher(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" {
		res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)
		return res, nil
	}

	metadata, err := store.GetVoucherData(ctx, h.prefixDb, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve voucher data: %v", err)
	}

	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		return res, nil
	}

	if err := store.StoreTemporaryVoucher(ctx, h.userdataStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on StoreTemporaryVoucher", "error", err)
		return res, err
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)
	res.Content = l.Get("Symbol: %s\nBalance: %s", metadata.TokenSymbol, metadata.Balance)

	return res, nil
}

// SetVoucher retrieves the temp voucher data and sets it as the active data.
func (h *MenuHandlers) SetVoucher(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// Get temporary data
	tempData, err := store.GetTemporaryVoucherData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTemporaryVoucherData", "error", err)
		return res, err
	}

	// Set as active and clear temporary data
	if err := store.UpdateVoucherData(ctx, h.userdataStore, sessionId, tempData); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateVoucherData", "error", err)
		return res, err
	}

	res.Content = tempData.TokenSymbol
	return res, nil
}

// GetVoucherDetails retrieves the voucher details.
func (h *MenuHandlers) GetVoucherDetails(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	// get the active address
	activeAddress, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeAddress entry with", "key", storedb.DATA_ACTIVE_ADDRESS, "error", err)
		return res, err
	}

	// use the voucher contract address to get the data from the API
	voucherData, err := h.accountService.VoucherData(ctx, string(activeAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		return res, nil
	}

	res.Content = fmt.Sprintf(
		"Name: %s\nSymbol: %s\nCommodity: %s\nLocation: %s", voucherData.TokenName, voucherData.TokenSymbol, voucherData.TokenCommodity, voucherData.TokenLocation,
	)

	return res, nil
}

// CheckTransactions retrieves the transactions from the API using the "PublicKey" and stores to prefixDb.
func (h *MenuHandlers) CheckTransactions(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_no_transfers, _ := h.flagManager.GetFlag("flag_no_transfers")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")

	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Fetch transactions from the API using the public key
	transactionsResp, err := h.accountService.FetchTransactions(ctx, string(publicKey))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on FetchTransactions", "error", err)
		return res, err
	}

	// Return if there are no transactions
	if len(transactionsResp) == 0 {
		res.FlagSet = append(res.FlagSet, flag_no_transfers)
		return res, nil
	}

	data := store.ProcessTransfers(transactionsResp)

	// Store all transaction data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_TX_SENDERS:    data.Senders,
		storedb.DATA_TX_RECIPIENTS: data.Recipients,
		storedb.DATA_TX_VALUES:     data.TransferValues,
		storedb.DATA_TX_ADDRESSES:  data.Addresses,
		storedb.DATA_TX_HASHES:     data.TxHashes,
		storedb.DATA_TX_DATES:      data.Dates,
		storedb.DATA_TX_SYMBOLS:    data.Symbols,
		storedb.DATA_TX_DECIMALS:   data.Decimals,
	}

	for key, value := range dataMap {
		if err := h.prefixDb.Put(ctx, []byte(storedb.ToBytes(key)), []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "failed to write to prefixDb", "error", err)
			return res, err
		}
	}

	res.FlagReset = append(res.FlagReset, flag_no_transfers)

	return res, nil
}

// GetTransactionsList fetches the list of transactions and formats them.
func (h *MenuHandlers) GetTransactionsList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Read transactions from the store and format them
	TransactionSenders, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_SENDERS))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionSenders from prefixDb", "error", err)
		return res, err
	}
	TransactionSyms, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_SYMBOLS))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionSyms from prefixDb", "error", err)
		return res, err
	}
	TransactionValues, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_VALUES))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionValues from prefixDb", "error", err)
		return res, err
	}
	TransactionDates, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_DATES))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionDates from prefixDb", "error", err)
		return res, err
	}

	// Parse the data
	senders := strings.Split(string(TransactionSenders), "\n")
	syms := strings.Split(string(TransactionSyms), "\n")
	values := strings.Split(string(TransactionValues), "\n")
	dates := strings.Split(string(TransactionDates), "\n")

	var formattedTransactions []string
	for i := 0; i < len(senders); i++ {
		sender := strings.TrimSpace(senders[i])
		sym := strings.TrimSpace(syms[i])
		value := strings.TrimSpace(values[i])
		date := strings.Split(strings.TrimSpace(dates[i]), " ")[0]

		status := "Received"
		if sender == string(publicKey) {
			status = "Sent"
		}

		// Use the ReplaceSeparator function for the menu separator
		transactionLine := fmt.Sprintf("%d%s%s %s %s %s", i+1, h.ReplaceSeparatorFunc(":"), status, value, sym, date)
		formattedTransactions = append(formattedTransactions, transactionLine)
	}

	res.Content = strings.Join(formattedTransactions, "\n")

	return res, nil
}

// ViewTransactionStatement retrieves the transaction statement
// and displays it to the user.
func (h *MenuHandlers) ViewTransactionStatement(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	flag_incorrect_statement, _ := h.flagManager.GetFlag("flag_incorrect_statement")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "11" || inputStr == "22" {
		res.FlagReset = append(res.FlagReset, flag_incorrect_statement)
		return res, nil
	}

	// Convert input string to integer
	index, err := strconv.Atoi(strings.TrimSpace(inputStr))
	if err != nil {
		return res, fmt.Errorf("invalid input: must be a number between 1 and 10")
	}

	if index < 1 || index > 10 {
		return res, fmt.Errorf("invalid input: index must be between 1 and 10")
	}

	statement, err := store.GetTransferData(ctx, h.prefixDb, string(publicKey), index)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve transfer data: %v", err)
	}

	if statement == "" {
		res.FlagSet = append(res.FlagSet, flag_incorrect_statement)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_statement)
	res.Content = statement

	return res, nil
}

// persistInitialLanguageCode receives an initial language code and persists it to the store
func (h *MenuHandlers) persistInitialLanguageCode(ctx context.Context, sessionId string, code string) error {
	store := h.userdataStore
	_, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INITIAL_LANGUAGE_CODE)
	if err == nil {
		return nil
	}
	if !db.IsNotFound(err) {
		return err
	}
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_INITIAL_LANGUAGE_CODE, []byte(code))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to persist initial language code", "key", storedb.DATA_INITIAL_LANGUAGE_CODE, "value", code, "error", err)
		return err
	}
	return nil
}

// persistLanguageCode persists the selected ISO 639 language code
func (h *MenuHandlers) persistLanguageCode(ctx context.Context, code string) error {
	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return fmt.Errorf("missing session")
	}
	err := store.WriteEntry(ctx, sessionId, storedb.DATA_SELECTED_LANGUAGE_CODE, []byte(code))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to persist language code", "key", storedb.DATA_SELECTED_LANGUAGE_CODE, "value", code, "error", err)
		return err
	}
	return h.persistInitialLanguageCode(ctx, sessionId, code)
}

// constructAccountAlias retrieves and alias based on the first and family name
// and writes the result in DATA_ACCOUNT_ALIAS
func (h *MenuHandlers) constructAccountAlias(ctx context.Context) error {
	var alias string
	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return fmt.Errorf("missing session")
	}
	firstName, err := store.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME)
	if err != nil {
		if db.IsNotFound(err) {
			return nil
		}
		return err
	}
	familyName, err := store.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME)
	if err != nil {
		if db.IsNotFound(err) {
			return nil
		}
		return err
	}
	pubKey, err := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		if db.IsNotFound(err) {
			return nil
		}
		return err
	}
	aliasInput := fmt.Sprintf("%s%s", firstName, familyName)
	aliasResult, err := h.accountService.RequestAlias(ctx, string(pubKey), aliasInput)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to retrieve alias", "alias", aliasInput, "error_alias_request", err)
		return fmt.Errorf("Failed to retrieve alias: %s", err.Error())
	}
	alias = aliasResult.Alias
	//Store the alias
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS, []byte(alias))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write account alias", "key", storedb.DATA_ACCOUNT_ALIAS, "value", alias, "error", err)
		return err
	}
	return nil
}

// RequestCustomAlias requests an ENS based alias name based on a user's input,then saves it as temporary value
func (h *MenuHandlers) RequestCustomAlias(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	if string(input) == "0" {
		return res, nil
	}

	store := h.userdataStore
	aliasHint, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		if db.IsNotFound(err) {
			return res, nil
		}
		return res, err
	}
	//Ensures that the call doesn't happen twice for the same alias hint
	if !bytes.Equal(aliasHint, input) {
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(string(input)))
		if err != nil {
			return res, err
		}
		pubKey, err := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
		if err != nil {
			if db.IsNotFound(err) {
				return res, nil
			}
		}
		aliasResult, err := h.accountService.RequestAlias(ctx, string(pubKey), string(input))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to retrieve alias", "alias", string(aliasHint), "error_alias_request", err)
			return res, fmt.Errorf("Failed to retrieve alias: %s", err.Error())
		}
		alias := aliasResult.Alias

		//Store the returned alias,wait for user to confirm it as new account alias
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(alias))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write account alias", "key", storedb.DATA_TEMPORARY_VALUE, "value", alias, "error", err)
			return res, err
		}
	}
	return res, nil
}

// GetSuggestedAlias loads and displays the suggested alias name from the temporary value
func (h *MenuHandlers) GetSuggestedAlias(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	suggestedAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		return res, nil
	}
	res.Content = string(suggestedAlias)
	return res, nil
}

// ConfirmNewAlias  reads  the suggested alias from the temporary value and confirms it  as the new account alias.
func (h *MenuHandlers) ConfirmNewAlias(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	flag_alias_set, _ := h.flagManager.GetFlag("flag_alias_set")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	newAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		return res, nil
	}
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS, []byte(string(newAlias)))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to clear DATA_ACCOUNT_ALIAS_VALUE entry with", "key", storedb.DATA_ACCOUNT_ALIAS, "value", "empty", "error", err)
		return res, err
	}

	res.FlagSet = append(res.FlagSet, flag_alias_set)
	return res, nil
}

// ClearTemporaryValue empties the DATA_TEMPORARY_VALUE at the main menu to prevent
// previously stored data from being accessed
func (h *MenuHandlers) ClearTemporaryValue(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	userStore := h.userdataStore

	// clear the temporary value at the start
	err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(""))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to clear DATA_TEMPORARY_VALUE entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", "empty", "error", err)
		return res, err
	}
	return res, nil
}
