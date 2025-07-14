package application

import (
	"context"
	"fmt"
	"strconv"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/common/phone"
	"git.grassecon.net/grassrootseconomics/common/pin"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

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
