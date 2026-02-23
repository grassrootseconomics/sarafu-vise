package application

import (
	"context"
	"fmt"
	"strconv"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
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

	content, err = loadUserContent(ctx, string(activeSym), string(activeBal), string(accAlias))
	if err != nil {
		return res, err
	}
	res.Content = content

	return res, nil
}

// loadUserContent loads the main user content in the main menu: the alias, balance and active symbol associated with active voucher
func loadUserContent(ctx context.Context, activeSym, balance, alias string) (string, error) {
	var content string

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	// Format the balance to 2 decimal places or default to 0.00
	formattedAmount, err := store.TruncateDecimalString(balance, 2)
	if err != nil {
		formattedAmount = "0.00"
	}

	// format the final outputs
	balStr := fmt.Sprintf("%s %s", formattedAmount, activeSym)

	if alias != "" {
		content = l.Get("%s\nBalance: %s\n", alias, balStr)
	} else {
		content = l.Get("Balance: %s\n", balStr)
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

// CalculateCreditAndDebt calls the API to get the credit and debt
// uses the pretium rates to convert the value to Ksh
func (h *MenuHandlers) CalculateCreditAndDebt(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	// Fetch session data
	_, activeBal, activeSym, activeAddress, publicKey, activeDecimal, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		res.Content = l.Get("Credit: %s KSH\nDebt: %s %s\n", "0", "0", string(activeSym))
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_api_call_error)

	// Resolve active pool
	activePoolAddress, _, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// Fetch swappable vouchers (pool view)
	swappableVouchers, err := h.accountService.GetPoolSwappableFromVouchers(ctx, string(activePoolAddress), string(publicKey))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetPoolSwappableFromVouchers", "error", err)
		res.Content = l.Get("Credit: %s KSH\nDebt: %s %s\n", "0", "0", string(activeSym))
		return res, nil
	}

	if len(swappableVouchers) == 0 {
		res.Content = l.Get("Credit: %s KSH\nDebt: %s %s\n", "0", "0", string(activeSym))
		return res, nil
	}

	// Fetch ALL wallet vouchers (voucher holdings view)
	allVouchers, err := h.accountService.FetchVouchers(ctx, string(publicKey))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on FetchVouchers", "error", err)
		return res, nil
	}

	// CREDIT calculation
	// Rule:
	// 1. Swap quote of active voucher → first stable in pool from GetPoolSwappableFromVouchers
	// 2. PLUS all stable balances from FetchVouchers

	scaledCredit := "0"

	// 1️. Find first stable voucher in POOL (for swap target)
	var firstPoolStable *dataserviceapi.TokenHoldings
	for i := range swappableVouchers {
		if isStableVoucher(swappableVouchers[i].TokenAddress) {
			firstPoolStable = &swappableVouchers[i]
			break
		}
	}

	// 2️. If pool has a stable, get swap quote
	if firstPoolStable != nil {
		finalAmountStr, err := store.ParseAndScaleAmount(
			string(activeBal),
			string(activeDecimal),
		)
		if err != nil {
			return res, err
		}

		// swap active -> FIRST stable from pool list
		r, err := h.accountService.GetPoolSwapQuote(ctx, finalAmountStr, string(publicKey), string(activeAddress), string(activePoolAddress), firstPoolStable.TokenAddress)
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_call_error)
			res.Content = l.Get("Your request failed. Please try again later.")
			logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
			return res, nil
		}

		// scale using REAL stable decimals
		finalQuote := store.ScaleDownBalance(r.OutValue, firstPoolStable.TokenDecimals)

		scaledCredit = store.AddDecimalStrings(scaledCredit, finalQuote)
	}

	// 3️. Add ALL wallet stable balances (from FetchVouchers)
	for _, v := range allVouchers {
		if isStableVoucher(v.TokenAddress) {
			scaled := store.ScaleDownBalance(v.Balance, v.TokenDecimals)
			scaledCredit = store.AddDecimalStrings(scaledCredit, scaled)
		}
	}

	// DEBT calculation
	// Rule:
	// - Default = 0
	// - If active is stable → remain 0
	// - If active is non-stable and exists in pool → use pool balance

	scaledDebt := "0"

	if !isStableVoucher(string(activeAddress)) {
		for _, v := range swappableVouchers {
			if v.TokenSymbol == string(activeSym) {
				scaledDebt = store.ScaleDownBalance(v.Balance, v.TokenDecimals)
				break
			}
		}
	}

	formattedDebt, _ := store.TruncateDecimalString(scaledDebt, 2)

	// Fetch MPESA rates
	rates, err := h.accountService.GetMpesaOnrampRates(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on GetMpesaOnrampRates", "error", err)
		return res, nil
	}

	creditFloat, _ := strconv.ParseFloat(scaledCredit, 64)
	creditKsh := fmt.Sprintf("%f", creditFloat*rates.Buy)
	kshFormattedCredit, _ := store.TruncateDecimalString(creditKsh, 0)

	res.Content = l.Get(
		"Credit: %s KSH\nDebt: %s %s\n",
		kshFormattedCredit,
		formattedDebt,
		string(activeSym),
	)

	return res, nil
}
