package application

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"

	"git.grassecon.net/grassrootseconomics/common/hex"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

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
