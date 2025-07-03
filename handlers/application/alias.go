package application

import (
	"bytes"
	"context"
	"fmt"
	"unicode"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

// RequestCustomAlias requests an ENS based alias name based on a user's input,then saves it as temporary value
func (h *MenuHandlers) RequestCustomAlias(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var alias string
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
		publicKey, err := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
		if err != nil {
			if db.IsNotFound(err) {
				return res, nil
			}
		}
		sanitizedInput := sanitizeAliasHint(string(input))
		// Check if an alias already exists
		existingAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_SUGGESTED_ALIAS)
		if err == nil && len(existingAlias) > 0 {
			logg.InfoCtxf(ctx, "Current alias", "alias", string(existingAlias))
			// Update existing alias
			aliasResult, err := h.accountService.UpdateAlias(ctx, sanitizedInput, string(publicKey))
			if err != nil {
				res.FlagSet = append(res.FlagSet, flag_api_error)
				logg.ErrorCtxf(ctx, "failed to update alias", "alias", sanitizedInput, "error", err)
				return res, nil
			}
			alias = aliasResult.Alias
			logg.InfoCtxf(ctx, "Updated alias", "alias", alias)
		} else {
			logg.InfoCtxf(ctx, "Registering a new alias", "err", err)
			// Register a new alias
			aliasResult, err := h.accountService.RequestAlias(ctx, string(publicKey), sanitizedInput)
			if err != nil {
				res.FlagSet = append(res.FlagSet, flag_api_error)
				logg.ErrorCtxf(ctx, "failed to retrieve alias", "alias", sanitizedInput, "error_alias_request", err)
				return res, nil
			}
			res.FlagReset = append(res.FlagReset, flag_api_error)

			alias = aliasResult.Alias
			logg.InfoCtxf(ctx, "Suggested alias", "alias", alias)
		}
		//Store the returned alias,wait for user to confirm it as new account alias
		logg.InfoCtxf(ctx, "Final suggested alias", "alias", alias)
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_SUGGESTED_ALIAS, []byte(alias))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write suggested alias", "key", storedb.DATA_SUGGESTED_ALIAS, "value", alias, "error", err)
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
	if err != nil && len(suggestedAlias) <= 0 {
		logg.ErrorCtxf(ctx, "failed to read suggested alias", "key", storedb.DATA_SUGGESTED_ALIAS, "error", err)
		return res, nil
	}
	res.Content = string(suggestedAlias)
	return res, nil
}

// ConfirmNewAlias reads the suggested alias from the [DATA_SUGGECTED_ALIAS] key and confirms it  as the new account alias.
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
