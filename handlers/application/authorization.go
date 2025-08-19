package application

import (
	"context"
	"fmt"

	"git.grassecon.net/grassrootseconomics/common/pin"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/go-vise/resource"
)

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
