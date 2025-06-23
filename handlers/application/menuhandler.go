package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"

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
	"git.grassecon.net/grassrootseconomics/sarafu-vise/internal/sms"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/profile"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/ethutils"
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
	smsService           sms.SmsService
	logDb                store.LogDb
	profile              *profile.Profile
	ReplaceSeparatorFunc func(string) string
}

// NewHandlers creates a new instance of the Handlers struct with the provided dependencies.
func NewMenuHandlers(appFlags *FlagManager, userdataStore db.Db, logdb db.Db, accountService remote.AccountService, replaceSeparatorFunc func(string) string) (*MenuHandlers, error) {
	if userdataStore == nil {
		return nil, fmt.Errorf("cannot create handler with nil userdata store")
	}
	userDb := &store.UserDataStore{
		Db: userdataStore,
	}
	smsservice := sms.SmsService{
		Accountservice: accountService,
		Userdatastore:  *userDb,
	}

	logDb := store.LogDb{
		Db: logdb,
	}

	// Instantiate the SubPrefixDb with "DATATYPE_USERDATA" prefix
	prefix := storedb.ToBytes(db.DATATYPE_USERDATA)
	prefixDb := storedb.NewSubPrefixDb(userdataStore, prefix)

	h := &MenuHandlers{
		userdataStore:        userDb,
		flagManager:          appFlags,
		accountService:       accountService,
		smsService:           smsservice,
		prefixDb:             prefixDb,
		logDb:                logDb,
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
	flag_account_creation_failed, _ := h.flagManager.GetFlag("flag_account_creation_failed")

	r, err := h.accountService.CreateAccount(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_account_creation_failed)
		logg.ErrorCtxf(ctx, "failed to create an account", "error", err)
		return nil
	}
	res.FlagReset = append(res.FlagReset, flag_account_creation_failed)

	trackingId := r.TrackingId
	publicKey := r.PublicKey

	data := map[storedb.DataTyp]string{
		storedb.DATA_TRACKING_ID:   trackingId,
		storedb.DATA_PUBLIC_KEY:    publicKey,
		storedb.DATA_ACCOUNT_ALIAS: "",
	}
	store := h.userdataStore
	logdb := h.logDb
	for key, value := range data {
		err = store.WriteEntry(ctx, sessionId, key, []byte(value))
		if err != nil {
			return err
		}
		err = logdb.WriteLogEntry(ctx, sessionId, key, []byte(value))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write log entry", "key", key, "value", value)
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

	err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY_REVERSE, []byte(sessionId))
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to write log entry", "key", storedb.DATA_PUBLIC_KEY_REVERSE, "value", sessionId)
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

func (h *MenuHandlers) CheckAccountCreated(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_language_set, _ := h.flagManager.GetFlag("flag_language_set")
	flag_account_created, _ := h.flagManager.GetFlag("flag_account_created")

	store := h.userdataStore

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	_, err := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		if db.IsNotFound(err) {
			// reset major flags
			res.FlagReset = append(res.FlagReset, flag_language_set)
			res.FlagReset = append(res.FlagReset, flag_account_created)

			return res, nil
		}

		return res, nil
	}

	res.FlagSet = append(res.FlagSet, flag_account_created)
	return res, nil
}

// CheckBlockedStatus:
// 1. Checks whether the DATA_SELF_PIN_RESET is 1 and sets the flag_account_pin_reset
// 2. resets the account blocked flag if the PIN attempts have been reset by an admin.
func (h *MenuHandlers) CheckBlockedStatus(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	flag_account_blocked, _ := h.flagManager.GetFlag("flag_account_blocked")
	flag_account_pin_reset, _ := h.flagManager.GetFlag("flag_account_pin_reset")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	res.FlagReset = append(res.FlagReset, flag_account_pin_reset)

	selfPinReset, err := store.ReadEntry(ctx, sessionId, storedb.DATA_SELF_PIN_RESET)
	if err == nil {
		pinResetValue, _ := strconv.ParseUint(string(selfPinReset), 0, 64)
		if pinResetValue == 1 {
			res.FlagSet = append(res.FlagSet, flag_account_pin_reset)
		}
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

	flag_invalid_pin, _ := h.flagManager.GetFlag("flag_invalid_pin")

	if string(input) == "0" {
		return res, nil
	}

	accountPIN := string(input)

	// Validate that the PIN has a valid format.
	if !pin.IsValidPIN(accountPIN) {
		res.FlagSet = append(res.FlagSet, flag_invalid_pin)
		return res, nil
	}
	res.FlagReset = append(res.FlagReset, flag_invalid_pin)

	// Hash the PIN
	hashedPIN, err := pin.HashPIN(string(accountPIN))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to hash the PIN", "error", err)
		return res, err
	}

	store := h.userdataStore
	logdb := h.logDb

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write temporaryAccountPIN entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", accountPIN, "error", err)
		return res, err
	}

	err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to write temporaryAccountPIN log entry", "key", storedb.DATA_TEMPORARY_VALUE, "value", accountPIN, "error", err)
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

// ResetApiCallFailure resets the api call failure flag
func (h *MenuHandlers) ResetApiCallFailure(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
	res.FlagReset = append(res.FlagReset, flag_api_error)
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
	flag_account_pin_reset, _ := h.flagManager.GetFlag("flag_account_pin_reset")

	if string(input) == "0" {
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
		return res, nil
	}

	store := h.userdataStore
	logdb := h.logDb
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

	err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN, []byte(hashedTemporaryPin))
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to write AccountPIN log entry", "key", storedb.DATA_ACCOUNT_PIN, "value", hashedTemporaryPin, "error", err)
	}

	// set the DATA_SELF_PIN_RESET as 0
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_SELF_PIN_RESET, []byte("0"))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write DATA_SELF_PIN_RESET entry with", "key", storedb.DATA_SELF_PIN_RESET, "self PIN reset value", "0", "error", err)
		return res, err
	}
	res.FlagReset = append(res.FlagReset, flag_account_pin_reset)

	return res, nil
}

// ValidateBlockedNumber performs validation of phone numbers during the Reset other's PIN.
// It checks phone number format and verifies registration status.
// If valid, it writes the number under DATA_BLOCKED_NUMBER on the admin account
func (h *MenuHandlers) ValidateBlockedNumber(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	flag_unregistered_number, _ := h.flagManager.GetFlag("flag_unregistered_number")
	store := h.userdataStore
	logdb := h.logDb
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

	err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER, []byte(formattedNumber))
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to write blocked number log entry", "key", storedb.DATA_BLOCKED_NUMBER, "value", formattedNumber, "error", err)
	}

	return res, nil
}

// ResetOthersPin handles the PIN reset process for other users' accounts by:
// 1. Retrieving the blocked phone number from the session
// 2. Writing the DATA_SELF_PIN_RESET on the blocked phone number
// 3. Resetting the DATA_INCORRECT_PIN_ATTEMPTS to 0 for the blocked phone number
func (h *MenuHandlers) ResetOthersPin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	store := h.userdataStore
	smsservice := h.smsService

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	blockedPhonenumber, err := store.ReadEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read blockedPhonenumber entry with", "key", storedb.DATA_BLOCKED_NUMBER, "error", err)
		return res, err
	}

	// set the DATA_SELF_PIN_RESET for the account
	err = store.WriteEntry(ctx, string(blockedPhonenumber), storedb.DATA_SELF_PIN_RESET, []byte("1"))
	if err != nil {
		return res, nil
	}

	err = store.WriteEntry(ctx, string(blockedPhonenumber), storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(string("0")))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to reset incorrect PIN attempts", "key", storedb.DATA_INCORRECT_PIN_ATTEMPTS, "error", err)
		return res, err
	}
	blockedPhoneStr := string(blockedPhonenumber)
	//Trigger an SMS to inform a user that the  blocked account has been reset
	if phone.IsValidPhoneNumber(blockedPhoneStr) {
		err = smsservice.SendPINResetSMS(ctx, sessionId, blockedPhoneStr)
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to send PIN reset SMS", "error", err)
			return res, nil
		}
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
	err := store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(string("0")))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to reset incorrect PIN attempts ", "key", storedb.DATA_INCORRECT_PIN_ATTEMPTS, "error", err)
		return err
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

// VerifyCreatePin checks whether the confirmation PIN is similar to the temporary PIN
// If similar, it sets the USERFLAG_PIN_SET flag and writes the account PIN allowing the user
// to access the main menu.
func (h *MenuHandlers) VerifyCreatePin(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_valid_pin, _ := h.flagManager.GetFlag("flag_valid_pin")
	flag_pin_mismatch, _ := h.flagManager.GetFlag("flag_pin_mismatch")
	flag_pin_set, _ := h.flagManager.GetFlag("flag_pin_set")

	if string(input) == "0" {
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
		return res, nil
	}

	store := h.userdataStore
	logdb := h.logDb

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
		res.FlagSet = append(res.FlagSet, flag_valid_pin)
		res.FlagSet = append(res.FlagSet, flag_pin_set)
		res.FlagReset = append(res.FlagReset, flag_pin_mismatch)
	} else {
		res.FlagSet = append(res.FlagSet, flag_pin_mismatch)
		return res, nil
	}

	// save the hashed PIN as the new account PIN
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN, []byte(hashedTemporaryPin))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write DATA_ACCOUNT_PIN entry with", "key", storedb.DATA_ACCOUNT_PIN, "value", hashedTemporaryPin, "error", err)
		return res, err
	}

	err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN, []byte(hashedTemporaryPin))
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to write DATA_ACCOUNT_PIN log entry", "key", storedb.DATA_ACCOUNT_PIN, "value", hashedTemporaryPin, "error", err)
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
	logdb := h.logDb

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

		err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_FIRST_NAME, []byte(temporaryFirstName))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write firtname db log entry", "key", storedb.DATA_FIRST_NAME, "value", temporaryFirstName)
		}
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
	logdb := h.logDb
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

		err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME, []byte(temporaryFamilyName))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write firtname db log entry", "key", storedb.DATA_FAMILY_NAME, "value", temporaryFamilyName)
		}
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
	logdb := h.logDb

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

		err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_YOB, []byte(temporaryYob))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write yob db log entry", "key", storedb.DATA_YOB, "value", temporaryYob)
		}
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
	logdb := h.logDb

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

		err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_LOCATION, []byte(temporaryLocation))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write location db log entry", "key", storedb.DATA_LOCATION, "value", temporaryLocation)
		}
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
	logdb := h.logDb
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

		err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_GENDER, []byte(temporaryGender))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write gender db log entry", "key", storedb.DATA_TEMPORARY_VALUE, "value", temporaryGender)
		}

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
	logdb := h.logDb

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

		err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_FIRST_NAME, []byte(temporaryOfferings))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write offerings db log entry", "key", storedb.DATA_OFFERINGS, "value", offerings)
		}
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
	logg.InfoCtxf(ctx, "GetCurrentProfileInfo", "filename", filename, "dbKeyStr:", dbKeyStr)
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
		alias := string(profileInfo)
		if alias == "" {
			res.Content = defaultValue
		} else {
			res.Content = alias
		}
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

	if alias != defaultValue && alias != "" {
		alias = strings.Split(alias, ".")[0]
	} else {
		alias = defaultValue
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
	err := h.insertProfileItems(ctx, sessionId, &res)
	if err != nil {
		return res, err
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

// CheckIdentifier retrieves the Public key from the userdatastore under the key: DATA_PUBLIC_KEY and triggers an sms that
// will be sent to the associated session id
func (h *MenuHandlers) CheckIdentifier(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	smsservice := h.smsService

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
	res.Content = string(publicKey)
	//trigger an address sms to be delivered to the associated session id
	err = smsservice.SendAddressSMS(ctx)
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to trigger an address sms", "error", err)
		return res, nil
	}

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

	pinInput := string(input)

	if !pin.IsValidPIN(pinInput) {
		res.FlagReset = append(res.FlagReset, flag_account_authorized, flag_allow_update)
		return res, nil
	}

	store := h.userdataStore
	AccountPin, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read AccountPin entry with", "key", storedb.DATA_ACCOUNT_PIN, "error", err)
		return res, err
	}

	// verify that the user provided the correct PIN
	if pin.VerifyPIN(string(AccountPin), pinInput) {
		// set the required flags for a valid PIN
		res.FlagSet = append(res.FlagSet, flag_allow_update, flag_account_authorized)
		res.FlagReset = append(res.FlagReset, flag_incorrect_pin)

		err := h.resetIncorrectPINAttempts(ctx, sessionId)
		if err != nil {
			return res, err
		}
	} else {
		// set the required flags for an incorrect PIN
		res.FlagSet = append(res.FlagSet, flag_incorrect_pin)
		res.FlagReset = append(res.FlagReset, flag_account_authorized, flag_allow_update)

		err = h.incrementIncorrectPINAttempts(ctx, sessionId)
		if err != nil {
			return res, err
		}
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
		return res, nil
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
		content = l.Get("Balance: %s\n", balStr)
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

	// get the active sym and active balance
	activeSym, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.InfoCtxf(ctx, "could not find the activeSym in checkBalance:", "err", err)
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
			return res, err
		}
	}

	activeBal, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
			return res, err
		}
	}

	accAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS)
	if err != nil {
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read account alias entry with", "key", storedb.DATA_ACCOUNT_ALIAS, "error", err)
			return res, err
		}
	} else {
		alias = strings.Split(string(accAlias), ".")[0]
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
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	// remove white spaces
	recipient := strings.ReplaceAll(string(input), " ", "")

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
			// checksum the address
			address := ethutils.ChecksumAddress(recipient)

			// Save the valid Ethereum address as the recipient
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(address))
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
						res.FlagReset = append(res.FlagReset, flag_api_error)
						AliasAddressResult = AliasAddress.Address
						continue
					} else {
						res.FlagSet = append(res.FlagSet, flag_api_error)
						logg.ErrorCtxf(ctx, "failed to resolve alias", "alias", recipient, "error_alias_check", err)
						return res, nil
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
	smsservice := h.smsService

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	recipient, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read invalid recipient info", "error", err)
		return res, err
	}

	if !phone.IsValidPhoneNumber(string(recipient)) {
		logg.InfoCtxf(ctx, "corrupted recipient", "key", storedb.DATA_TEMPORARY_VALUE, "recipient", recipient)
		return res, nil
	}

	_, err = smsservice.Accountservice.SendUpsellSMS(ctx, sessionId, string(recipient))
	if err != nil {
		res.Content = l.Get("Your invite request for %s to Sarafu Network failed. Please try again later.", string(recipient))
		return res, nil
	}
	res.Content = l.Get("Your invitation to %s to join Sarafu Network has been sent.", string(recipient))
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

// ManageVouchers retrieves the token holdings from the API using the "PublicKey" and
// 1. sets the first as the default voucher if no active voucher is set.
// 2. Stores list of vouchers
// 3. updates the balance of the active voucher
func (h *MenuHandlers) ManageVouchers(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	userStore := h.userdataStore
	logdb := h.logDb

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_no_active_voucher, _ := h.flagManager.GetFlag("flag_no_active_voucher")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Fetch vouchers from API
	vouchersResp, err := h.accountService.FetchVouchers(ctx, string(publicKey))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		return res, nil
	}
	res.FlagReset = append(res.FlagReset, flag_api_error)

	if len(vouchersResp) == 0 {
		res.FlagSet = append(res.FlagSet, flag_no_active_voucher)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_no_active_voucher)

	// Check if user has an active voucher with proper error handling
	activeSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		if db.IsNotFound(err) {
			// No active voucher, set the first one as default
			firstVoucher := vouchersResp[0]
			defaultSym := firstVoucher.TokenSymbol
			defaultBal := firstVoucher.Balance
			defaultDec := firstVoucher.TokenDecimals
			defaultAddr := firstVoucher.TokenAddress

			// Scale down the balance
			scaledBalance := store.ScaleDownBalance(defaultBal, defaultDec)

			firstVoucherMap := map[storedb.DataTyp]string{
				storedb.DATA_ACTIVE_SYM:     defaultSym,
				storedb.DATA_ACTIVE_BAL:     scaledBalance,
				storedb.DATA_ACTIVE_DECIMAL: defaultDec,
				storedb.DATA_ACTIVE_ADDRESS: defaultAddr,
			}

			for key, value := range firstVoucherMap {
				if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
					logg.ErrorCtxf(ctx, "Failed to write active voucher data", "key", key, "error", err)
					return res, err
				}
				err = logdb.WriteLogEntry(ctx, sessionId, key, []byte(value))
				if err != nil {
					logg.DebugCtxf(ctx, "Failed to write voucher db log entry", "key", key, "value", value)
				}
			}

			logg.InfoCtxf(ctx, "Default voucher set", "symbol", defaultSym, "balance", defaultBal, "decimals", defaultDec, "address", defaultAddr)
		} else {
			logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
			return res, err
		}
	} else {
		// Update active voucher balance
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

	// Store all voucher data
	data := store.ProcessVouchers(vouchersResp)
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_VOUCHER_SYMBOLS:   data.Symbols,
		storedb.DATA_VOUCHER_BALANCES:  data.Balances,
		storedb.DATA_VOUCHER_DECIMALS:  data.Decimals,
		storedb.DATA_VOUCHER_ADDRESSES: data.Addresses,
	}

	// Write data entries
	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	return res, nil
}

// GetVoucherList fetches the list of vouchers and formats them.
func (h *MenuHandlers) GetVoucherList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore

	// Read vouchers from the store
	voucherData, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_VOUCHER_SYMBOLS)
	logg.InfoCtxf(ctx, "reading GetVoucherList entries for sessionId: %s", sessionId, "key", storedb.DATA_VOUCHER_SYMBOLS, "voucherData", voucherData)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read voucherData entires with", "key", storedb.DATA_VOUCHER_SYMBOLS, "error", err)
		return res, err
	}

	formattedData := h.ReplaceSeparatorFunc(string(voucherData))

	logg.InfoCtxf(ctx, "final output for sessionId: %s", sessionId, "key", storedb.DATA_VOUCHER_SYMBOLS, "formattedData", formattedData)

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

	metadata, err := store.GetVoucherData(ctx, h.userdataStore, sessionId, inputStr)
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
	res.FlagReset = append(res.FlagReset, flag_api_error)

	res.Content = fmt.Sprintf(
		"Name: %s\nSymbol: %s\nCommodity: %s\nLocation: %s", voucherData.TokenName, voucherData.TokenSymbol, voucherData.TokenCommodity, voucherData.TokenLocation,
	)

	return res, nil
}

// GetDefaultPool returns the current user's Pool. If none is set, it returns the default config pool.
func (h *MenuHandlers) GetDefaultPool(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore
	activePoolSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM)
	if err != nil {
		if db.IsNotFound(err) {
			// set the default as the response
			res.Content = config.DefaultPoolSymbol()
			return res, nil
		}

		logg.ErrorCtxf(ctx, "failed to read the activePoolSym entry with", "key", storedb.DATA_ACTIVE_POOL_SYM, "error", err)
		return res, err
	}

	res.Content = string(activePoolSym)

	return res, nil
}

// ViewPool retrieves the pool details from the user store
// and displays it to the user for them to select it.
func (h *MenuHandlers) ViewPool(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_pool, _ := h.flagManager.GetFlag("flag_incorrect_pool")

	inputStr := string(input)

	poolData, err := store.GetPoolData(ctx, h.userdataStore, sessionId, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve pool data: %v", err)
	}

	if poolData == nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

		// no match found. Call the API using the inputStr as the symbol
		poolResp, err := h.accountService.RetrievePoolDetails(ctx, inputStr)
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_error)
			return res, nil
		}

		if len(poolResp.PoolSymbol) == 0 {
			// If the API does not return the data, set the flag
			res.FlagSet = append(res.FlagSet, flag_incorrect_pool)
			return res, nil
		}

		poolData = poolResp
	}

	if err := store.StoreTemporaryPool(ctx, h.userdataStore, sessionId, poolData); err != nil {
		logg.ErrorCtxf(ctx, "failed on StoreTemporaryPool", "error", err)
		return res, err
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_pool)
	res.Content = l.Get("Name: %s\nSymbol: %s", poolData.PoolName, poolData.PoolSymbol)

	return res, nil
}

// SetPool retrieves the temp pool data and sets it as the active data.
func (h *MenuHandlers) SetPool(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// Get temporary data
	tempData, err := store.GetTemporaryPoolData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTemporaryPoolData", "error", err)
		return res, err
	}

	// Set as active and clear temporary data
	if err := store.UpdatePoolData(ctx, h.userdataStore, sessionId, tempData); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdatePoolData", "error", err)
		return res, err
	}

	res.Content = tempData.PoolSymbol
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
	logdb := h.logDb
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
	res.FlagReset = append(res.FlagReset, flag_api_error)

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
		err = logdb.WriteLogEntry(ctx, sessionId, key, []byte(value))
		if err != nil {
			logg.DebugCtxf(ctx, "Failed to write tx db log entry", "key", key, "value", value)
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

	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

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
		sanitizedInput := sanitizeAliasHint(string(input))
		aliasResult, err := h.accountService.RequestAlias(ctx, string(pubKey), sanitizedInput)
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_error)
			logg.ErrorCtxf(ctx, "failed to retrieve alias", "alias", string(aliasHint), "error_alias_request", err)
			return res, nil
		}
		res.FlagReset = append(res.FlagReset, flag_api_error)

		alias := aliasResult.Alias
		logg.InfoCtxf(ctx, "Suggested alias ", "alias", alias)

		//Store the returned alias,wait for user to confirm it as new account alias
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_SUGGESTED_ALIAS, []byte(alias))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write account alias", "key", storedb.DATA_TEMPORARY_VALUE, "value", alias, "error", err)
			return res, err
		}
	}
	return res, nil
}

func sanitizeAliasHint(input string) string {
	for i, r := range input {
		// Check if the character is a special character (non-alphanumeric)
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return input[:i]
		}
	}
	// If no special character is found, return the whole input
	return input
}

// GetSuggestedAlias loads and displays the suggested alias name from the temporary value
func (h *MenuHandlers) GetSuggestedAlias(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	suggestedAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_SUGGESTED_ALIAS)
	if err != nil {
		return res, nil
	}
	res.Content = string(suggestedAlias)
	return res, nil
}

// ConfirmNewAlias  reads  the suggested alias from the [DATA_SUGGECTED_ALIAS] key and confirms it  as the new account alias.
func (h *MenuHandlers) ConfirmNewAlias(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore
	logdb := h.logDb

	flag_alias_set, _ := h.flagManager.GetFlag("flag_alias_set")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	newAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_SUGGESTED_ALIAS)
	if err != nil {
		return res, nil
	}
	logg.InfoCtxf(ctx, "Confirming new alias", "alias", string(newAlias))
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS, []byte(string(newAlias)))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to clear DATA_ACCOUNT_ALIAS_VALUE entry with", "key", storedb.DATA_ACCOUNT_ALIAS, "value", "empty", "error", err)
		return res, err
	}

	err = logdb.WriteLogEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS, []byte(newAlias))
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to write account alias db log entry", "key", storedb.DATA_ACCOUNT_ALIAS, "value", newAlias)
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

// GetPools fetches a list of 5 top pools
func (h *MenuHandlers) GetPools(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	userStore := h.userdataStore

	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")

	// call the api to get a list of top 5 pools sorted by swaps
	topPools, err := h.accountService.FetchTopPools(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on FetchTransactions", "error", err)
		return res, err
	}

	// Return if there are no pools
	if len(topPools) == 0 {
		return res, nil
	}

	data := store.ProcessPools(topPools)

	// Store all Pool data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_POOL_NAMES:     data.PoolNames,
		storedb.DATA_POOL_SYMBOLS:   data.PoolSymbols,
		storedb.DATA_POOL_ADDRESSES: data.PoolContractAdrresses,
	}

	// Write data entries
	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	res.Content = h.ReplaceSeparatorFunc(data.PoolSymbols)

	return res, nil
}

// LoadSwapFromList returns a list of possible vouchers to swap to
func (h *MenuHandlers) LoadSwapToList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore

	// get the active address and symbol
	activeAddress, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeAddress entry with", "key", storedb.DATA_ACTIVE_ADDRESS, "error", err)
		return res, err
	}
	activeSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
		return res, err
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")

	inputStr := string(input)
	if inputStr == "0" {
		return res, nil
	}

	// Get active pool address and symbol or fall back to default
	var activePoolAddress []byte
	activePoolAddress, err = userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_ADDRESS)
	if err != nil {
		if db.IsNotFound(err) {
			defaultPoolAddress := config.DefaultPoolAddress()
			// store the default as the active pool address
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_ADDRESS, []byte(defaultPoolAddress))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write default PoolContractAdrress", "key", storedb.DATA_ACTIVE_POOL_ADDRESS, "value", defaultPoolAddress, "error", err)
				return res, err
			}
			activePoolAddress = []byte(defaultPoolAddress)
		} else {
			logg.ErrorCtxf(ctx, "failed to read active PoolContractAdrress", "key", storedb.DATA_ACTIVE_POOL_ADDRESS, "error", err)
			return res, err
		}
	}

	var activePoolSymbol []byte
	activePoolSymbol, err = userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM)
	if err != nil {
		if db.IsNotFound(err) {
			defaultPoolSym := config.DefaultPoolName()
			// store the default as the active pool symbol
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM, []byte(defaultPoolSym))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write default Pool Symbol", "key", storedb.DATA_ACTIVE_POOL_SYM, "value", defaultPoolSym, "error", err)
				return res, err
			}
			activePoolSymbol = []byte(defaultPoolSym)
		} else {
			logg.ErrorCtxf(ctx, "failed to read active Pool symbol", "key", storedb.DATA_ACTIVE_POOL_SYM, "error", err)
			return res, err
		}
	}

	// call the api using the ActivePoolAddress and ActiveVoucherAddress to check if it is part of the pool
	r, err := h.accountService.CheckTokenInPool(ctx, string(activePoolAddress), string(activeAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on CheckTokenInPool", "error", err)
		return res, err
	}

	logg.InfoCtxf(ctx, "CheckTokenInPool", "response", r, "active_pool_address", string(activePoolAddress), "active_symbol_address", string(activeAddress))

	if !r.CanSwapFrom {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		res.Content = l.Get(
			"%s is not in %s. Please update your voucher and try again.",
			activeSym,
			activePoolSymbol,
		)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)

	// call the api using the activePoolAddress to get a list of SwapToSymbolsData
	swapToList, err := h.accountService.GetPoolSwappableVouchers(ctx, string(activePoolAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on FetchTransactions", "error", err)
		return res, err
	}

	logg.InfoCtxf(ctx, "GetPoolSwappableVouchers", "swapToList", swapToList)

	// Return if there are no vouchers
	if len(swapToList) == 0 {
		return res, nil
	}

	data := store.ProcessTokens(swapToList)

	logg.InfoCtxf(ctx, "ProcessTokens", "data", data)

	// Store all swap_to tokens data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_POOL_TO_SYMBOLS:   data.Symbols,
		storedb.DATA_POOL_TO_BALANCES:  data.Balances,
		storedb.DATA_POOL_TO_DECIMALS:  data.Decimals,
		storedb.DATA_POOL_TO_ADDRESSES: data.Addresses,
	}

	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	res.Content = h.ReplaceSeparatorFunc(data.Symbols)

	return res, nil
}

// SwapMaxLimit returns the max FROM token
// check if max/tokenDecimals > 0.1 for UX purposes and to prevent swapping of dust values
func (h *MenuHandlers) SwapMaxLimit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")
	flag_low_swap_amount, _ := h.flagManager.GetFlag("flag_low_swap_amount")

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher, flag_low_swap_amount)

	inputStr := string(input)
	if inputStr == "0" {
		return res, nil
	}

	userStore := h.userdataStore
	metadata, err := store.GetSwapToVoucherData(ctx, userStore, sessionId, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve swap to voucher data: %v", err)
	}
	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		return res, nil
	}

	logg.InfoCtxf(ctx, "Metadata from GetSwapToVoucherData:", "metadata", metadata)

	// Store the active swap_to data
	if err := store.UpdateSwapToVoucherData(ctx, userStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateSwapToVoucherData", "error", err)
		return res, err
	}

	swapData, err := store.ReadSwapData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	// call the api using the ActivePoolAddress, ActiveSwapFromAddress, ActiveSwapToAddress and PublicKey to get the swap max limit
	logg.InfoCtxf(ctx, "Call GetSwapFromTokenMaxLimit with:", "ActivePoolAddress", swapData.ActivePoolAddress, "ActiveSwapFromAddress", swapData.ActiveSwapFromAddress, "ActiveSwapToAddress", swapData.ActiveSwapToAddress, "publicKey", swapData.PublicKey)
	r, err := h.accountService.GetSwapFromTokenMaxLimit(ctx, swapData.ActivePoolAddress, swapData.ActiveSwapFromAddress, swapData.ActiveSwapToAddress, swapData.PublicKey)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on GetSwapFromTokenMaxLimit", "error", err)
		return res, err
	}

	// Scale down the amount
	maxAmountStr := store.ScaleDownBalance(r.Max, swapData.ActiveSwapFromDecimal)
	if err != nil {
		return res, err
	}

	maxAmountFloat, err := strconv.ParseFloat(maxAmountStr, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to parse maxAmountStr as float", "value", maxAmountStr, "error", err)
		return res, err
	}

	// Format to 2 decimal places
	maxStr := fmt.Sprintf("%.2f", maxAmountFloat)

	if maxAmountFloat < 0.1 {
		// return with low amount flag
		res.Content = maxStr
		res.FlagSet = append(res.FlagSet, flag_low_swap_amount)
		return res, nil
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, []byte(maxStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap max amount entry with", "key", storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, "value", maxStr, "error", err)
		return res, err
	}

	res.Content = fmt.Sprintf(
		"Maximum: %s\n\nEnter amount of %s to swap for %s:",
		maxStr, swapData.ActiveSwapFromSym, swapData.ActiveSwapToSym,
	)

	return res, nil
}

// SwapPreview displays the swap preview and estimates
func (h *MenuHandlers) SwapPreview(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	inputStr := string(input)
	if inputStr == "0" {
		return res, nil
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	maxValue, err := strconv.ParseFloat(swapData.ActiveSwapMaxAmount, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the swapMaxAmount to a float", "error", err)
		return res, err
	}

	inputAmount, err := strconv.ParseFloat(inputStr, 64)
	if err != nil || inputAmount > maxValue {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	finalAmountStr, err := store.ParseAndScaleAmount(inputStr, swapData.ActiveSwapFromDecimal)
	if err != nil {
		return res, err
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT, []byte(finalAmountStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", finalAmountStr, "error", err)
		return res, err
	}
	// store the user's input amount in the temporary value
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", finalAmountStr, "error", err)
		return res, err
	}

	// call the API to get the quote
	r, err := h.accountService.GetPoolSwapQuote(ctx, finalAmountStr, swapData.PublicKey, swapData.ActiveSwapFromAddress, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	// Scale down the quoted amount
	quoteAmountStr := store.ScaleDownBalance(r.OutValue, swapData.ActiveSwapToDecimal)
	qouteAmount, err := strconv.ParseFloat(quoteAmountStr, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to parse quoteAmountStr as float", "value", quoteAmountStr, "error", err)
		return res, err
	}

	// Format to 2 decimal places
	qouteStr := fmt.Sprintf("%.2f", qouteAmount)

	res.Content = fmt.Sprintf(
		"You will swap:\n%s %s for %s %s:",
		inputStr, swapData.ActiveSwapFromSym, qouteStr, swapData.ActiveSwapToSym,
	)

	return res, nil
}

// InitiateSwap calls the poolSwap and returns a confirmation based on the result.
func (h *MenuHandlers) InitiateSwap(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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

	userStore := h.userdataStore

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	swapAmount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read swapAmount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "error", err)
		return res, err
	}

	swapAmountStr := string(swapAmount)

	// Call the poolSwap API
	r, err := h.accountService.PoolSwap(ctx, swapAmountStr, swapData.PublicKey, swapData.ActiveSwapFromAddress, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	trackingId := r.TrackingId
	logg.InfoCtxf(ctx, "poolSwap", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. You will receive an SMS when your %s %s has been swapped for %s.",
		swapData.TemporaryValue,
		swapData.ActiveSwapFromSym,
		swapData.ActiveSwapToSym,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}
