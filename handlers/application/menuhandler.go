package application

import (
	"context"
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
	commonlang "git.grassecon.net/grassrootseconomics/common/lang"
	"git.grassecon.net/grassrootseconomics/common/pin"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/internal/sms"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/profile"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
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

// ResetApiCallFailure resets the api call failure flag
func (h *MenuHandlers) ResetApiCallFailure(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
	res.FlagReset = append(res.FlagReset, flag_api_error)
	return res, nil
}

// ResetUnregisteredNumber clears the unregistered number flag in the system,
// indicating that a number's registration status should no longer be marked as unregistered.
func (h *MenuHandlers) ResetUnregisteredNumber(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_unregistered_number, _ := h.flagManager.GetFlag("flag_unregistered_number")
	res.FlagReset = append(res.FlagReset, flag_unregistered_number)
	return res, nil
}

// ResetAllowUpdate resets the allowupdate flag that allows a user to update  profile data.
func (h *MenuHandlers) ResetAllowUpdate(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	res.FlagReset = append(res.FlagReset, flag_allow_update)
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
