package application

import (
	"bytes"
	"context"
	"fmt"
	"unicode"

	"github.com/grassrootseconomics/go-vise/db"
	"github.com/grassrootseconomics/go-vise/resource"

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
	flag_alias_unavailable, _ := h.flagManager.GetFlag("flag_alias_unavailable")

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
		existingAlias, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS)
		if err == nil && len(existingAlias) > 0 {
			logg.InfoCtxf(ctx, "Current alias", "alias", string(existingAlias))

			unavailable, err := h.isAliasUnavailable(ctx, sanitizedInput)
			if err == nil && unavailable {
				res.FlagSet = append(res.FlagSet, flag_alias_unavailable)
				res.FlagReset = append(res.FlagReset, flag_api_error)
				return res, nil
			}

			res.FlagReset = append(res.FlagReset, flag_alias_unavailable)

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

			unavailable, err := h.isAliasUnavailable(ctx, sanitizedInput)
			if err == nil && unavailable {
				res.FlagSet = append(res.FlagSet, flag_alias_unavailable)
				res.FlagReset = append(res.FlagReset, flag_api_error)
				return res, nil
			}

			res.FlagReset = append(res.FlagReset, flag_alias_unavailable)

			// Register a new alias
			aliasResult, err := h.accountService.RequestAlias(ctx, string(publicKey), sanitizedInput)
			if err != nil {
				res.FlagSet = append(res.FlagSet, flag_api_error)
				logg.ErrorCtxf(ctx, "failed to retrieve alias", "alias", sanitizedInput, "error_alias_request", err)
				return res, nil
			}
			res.FlagReset = append(res.FlagReset, flag_api_error)

			alias = aliasResult.Alias
			logg.InfoCtxf(ctx, "Registered alias", "alias", alias)
		}

		//Store the new account alias
		logg.InfoCtxf(ctx, "Final registered alias", "alias", alias)
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS, []byte(alias))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write account alias", "key", storedb.DATA_ACCOUNT_ALIAS, "value", alias, "error", err)
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

func (h *MenuHandlers) isAliasUnavailable(ctx context.Context, alias string) (bool, error) {
	fqdn := fmt.Sprintf("%s.%s", alias, "sarafu.eth")
	logg.InfoCtxf(ctx, "Checking if the fqdn alias is taken", "fqdn", fqdn)

	aliasAddress, err := h.accountService.CheckAliasAddress(ctx, fqdn)
	if err != nil {
		return false, err
	}
	if len(aliasAddress.Address) > 0 {
		return true, nil
	}

	return false, nil
}
