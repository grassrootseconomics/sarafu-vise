package application

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// CheckBalance retrieves the balance of the active voucher and sets
// the balance as the result content.
func (h *MenuHandlers) CheckBalance(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var (
		res     resource.Result
		err     error
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
	}

	// get current credit
	currentCredit, err := store.ReadEntry(ctx, sessionId, storedb.DATA_CURRENT_CREDIT)
	if err != nil {
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read currentCredit entry with", "key", storedb.DATA_CURRENT_CREDIT, "error", err)
			return res, err
		}
	}
	// get current debt
	currentDebt, err := store.ReadEntry(ctx, sessionId, storedb.DATA_CURRENT_DEBT)
	if err != nil {
		if !db.IsNotFound(err) {
			logg.ErrorCtxf(ctx, "failed to read currentDebt entry with", "key", storedb.DATA_CURRENT_DEBT, "error", err)
			return res, err
		}
	}

	content, err = loadUserContent(ctx, string(activeSym), string(activeBal), string(accAlias), string(currentCredit), string(currentDebt))
	if err != nil {
		return res, err
	}
	res.Content = content

	return res, nil
}

// loadUserContent loads the main user content in the main menu: the alias, balance and active symbol associated with active voucher
func loadUserContent(ctx context.Context, activeSym, balance, alias, currectCredit, currentDebt string) (string, error) {
	var content string

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	// Format the balance to 2 decimal places or default to 0.00
	formattedAmount, err := store.TruncateDecimalString(balance, 2)
	if err != nil {
		formattedAmount = "0.00"
	}

	formattedCredit, err := store.TruncateDecimalString(currectCredit, 0)
	if err != nil {
		formattedCredit = "0"
	}

	formattedDebt, err := store.TruncateDecimalString(currentDebt, 0)
	if err != nil {
		formattedDebt = "0"
	}

	// format the final outputs
	balStr := fmt.Sprintf("%s %s", formattedAmount, activeSym)
	creditStr := fmt.Sprintf("Credit: %s ksh", formattedCredit)
	debtStr := fmt.Sprintf("Debt: %s ksh", formattedDebt)

	if alias != "" {
		content = l.Get("%s\nBalance: %s\n%s\n%s", alias, balStr, creditStr, debtStr)
	} else {
		content = l.Get("Balance: %s\n%s\n%s", balStr, creditStr, debtStr)
	}
	return content, nil
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
