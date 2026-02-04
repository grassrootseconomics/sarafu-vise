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

	userStore := h.userdataStore

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
	flag_no_pay_debt_vouchers, _ := h.flagManager.GetFlag("flag_no_pay_debt_vouchers")

	// default response
	res.FlagReset = append(res.FlagReset, flag_api_call_error)
	res.Content = l.Get("Credit: %s KSH\nDebt: %s KSH\n", "0", "0")

	// Fetch session data
	_, _, activeSym, _, publicKey, _, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, nil
	}

	// Resolve active pool
	activePoolAddress, _, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// Fetch swappable vouchers
	swappableVouchers, err := h.accountService.GetPoolSwappableFromVouchers(ctx, string(activePoolAddress), string(publicKey))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetPoolSwappableFromVouchers", "error", err)
		return res, nil
	}

	if len(swappableVouchers) == 0 {
		return res, nil
	}

	// Filter stable vouchers (excluding active voucher)
	filteredSwappableVouchers := make([]dataserviceapi.TokenHoldings, 0)
	for _, v := range swappableVouchers {
		if v.TokenSymbol == string(activeSym) {
			continue
		}
		if isStableVoucher(v.TokenAddress) {
			filteredSwappableVouchers = append(filteredSwappableVouchers, v)
		}
	}

	// No stable vouchers → cannot pay debt
	if len(filteredSwappableVouchers) == 0 {
		res.FlagSet = append(res.FlagSet, flag_no_pay_debt_vouchers)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_no_pay_debt_vouchers)

	// Process stable vouchers for later use
	data := store.ProcessVouchers(filteredSwappableVouchers)

	// Find active voucher data
	activeSymStr := string(activeSym)
	var activeData *dataserviceapi.TokenHoldings
	for _, v := range swappableVouchers {
		if v.TokenSymbol == activeSymStr {
			activeData = &v
			break
		}
	}

	if activeData == nil {
		return res, nil
	}

	// Credit = active voucher balance
	scaledCredit := store.ScaleDownBalance(
		activeData.Balance,
		activeData.TokenDecimals,
	)

	// Debt = sum of stable vouchers only
	scaledDebt := "0"
	for _, v := range filteredSwappableVouchers {
		scaled := store.ScaleDownBalance(v.Balance, v.TokenDecimals)
		scaledDebt = store.AddDecimalStrings(scaledDebt, scaled)
	}

	// Fetch MPESA rates
	rates, err := h.accountService.GetMpesaOnrampRates(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on GetMpesaOnrampRates", "error", err)
		return res, nil
	}

	creditFloat, _ := strconv.ParseFloat(scaledCredit, 64)
	debtFloat, _ := strconv.ParseFloat(scaledDebt, 64)

	creditKsh := fmt.Sprintf("%f", creditFloat*rates.Buy)
	debtKsh := fmt.Sprintf("%f", debtFloat*rates.Buy)

	kshFormattedCredit, _ := store.TruncateDecimalString(creditKsh, 0)
	kshFormattedDebt, _ := store.TruncateDecimalString(debtKsh, 0)

	// Persist swap data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_POOL_TO_SYMBOLS:   data.Symbols,
		storedb.DATA_POOL_TO_BALANCES:  data.Balances,
		storedb.DATA_POOL_TO_DECIMALS:  data.Decimals,
		storedb.DATA_POOL_TO_ADDRESSES: data.Addresses,
	}

	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	res.Content = l.Get(
		"Credit: %s KSH\nDebt: %s KSH\n",
		kshFormattedCredit,
		kshFormattedDebt,
	)

	return res, nil
}
