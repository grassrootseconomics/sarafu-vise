package application

import (
	"context"
	"fmt"
	"strings"

	commonlang "git.grassecon.net/grassrootseconomics/common/lang"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/go-vise/db"
	"github.com/grassrootseconomics/go-vise/resource"
	"github.com/grassrootseconomics/go-vise/state"
)

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
