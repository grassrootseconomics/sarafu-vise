package application

import (
	"context"
	"fmt"
	"strconv"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

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
// 3. Sets key flags (language and PIN) if the data exists
func (h *MenuHandlers) CheckBlockedStatus(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore

	flag_account_blocked, _ := h.flagManager.GetFlag("flag_account_blocked")
	flag_account_pin_reset, _ := h.flagManager.GetFlag("flag_account_pin_reset")

	flag_pin_set, _ := h.flagManager.GetFlag("flag_pin_set")
	flag_language_set, _ := h.flagManager.GetFlag("flag_language_set")
	pinFlagSet := h.st.MatchFlag(flag_pin_set, true)
	languageFlagSet := h.st.MatchFlag(flag_language_set, true)

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// only check the data if the flag isn't set
	if !pinFlagSet {
		accountPin, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN)
		if len(accountPin) > 0 {
			res.FlagSet = append(res.FlagSet, flag_pin_set)
		}
	}
	if !languageFlagSet {
		languageCode, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_SELECTED_LANGUAGE_CODE)
		if len(languageCode) > 0 {
			res.FlagSet = append(res.FlagSet, flag_language_set)
		}
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
