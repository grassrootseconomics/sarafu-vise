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

	// set the default flag set/reset and content
	res.FlagReset = append(res.FlagReset, flag_api_call_error)
	res.Content = l.Get("Credit: %s KSH\nDebt: %s KSH\n", "0", "0")

	// Fetch session data
	_, _, activeSym, _, publicKey, _, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		// return if the user does not have an active voucher
		return res, nil
	}

	// Get active pool address and symbol or fall back to default
	activePoolAddress, _, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// call the api using the activePoolAddress to get a list of SwapToSymbolsData
	swappableVouchers, err := h.accountService.GetPoolSwappableFromVouchers(ctx, string(activePoolAddress), string(publicKey))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetPoolSwappableFromVouchers", "error", err)
		return res, nil
	}

	logg.InfoCtxf(ctx, "GetPoolSwappableFromVouchers", "swappable vouchers", swappableVouchers)

	// Return if there are no vouchers
	if len(swappableVouchers) == 0 {
		return res, nil
	}

	// Filter out the active voucher from swappableVouchers
	filteredSwappableVouchers := make([]dataserviceapi.TokenHoldings, 0, len(swappableVouchers))
	for _, s := range swappableVouchers {
		if s.TokenSymbol != string(activeSym) {
			filteredSwappableVouchers = append(filteredSwappableVouchers, s)
		}
	}

	// Store as filtered swap to list data (excluding the current active voucher) for future reference
	data := store.ProcessVouchers(filteredSwappableVouchers)

	logg.InfoCtxf(ctx, "ProcessVouchers", "data", data)

	// Find the matching voucher data
	activeSymStr := string(activeSym)
	var activeData *dataserviceapi.TokenHoldings
	for _, voucher := range swappableVouchers {
		if voucher.TokenSymbol == activeSymStr {
			activeData = &voucher
			break
		}
	}

	if activeData == nil {
		logg.InfoCtxf(ctx, "activeSym not found in vouchers, returning 0", "activeSym", activeSymStr)
		return res, nil
	}

	// Scale down the active balance (credit)
	// Max swappable value from pool using the active token
	scaledCredit := store.ScaleDownBalance(activeData.Balance, activeData.TokenDecimals)

	// Calculate total debt (sum of other vouchers)
	scaledDebt := "0"

	for _, voucher := range swappableVouchers {
		// Skip the active token
		if voucher.TokenSymbol == activeSymStr {
			continue
		}

		scaled := store.ScaleDownBalance(voucher.Balance, voucher.TokenDecimals)

		// Add scaled balances (decimal-safe)
		scaledDebt = store.AddDecimalStrings(scaledDebt, scaled)
	}

	// call the mpesa rates API to get the rates
	rates, err := h.accountService.GetMpesaOnrampRates(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on GetMpesaOnrampRates", "error", err)
		return res, nil
	}

	creditFloat, _ := strconv.ParseFloat(scaledCredit, 64)
	creditksh := fmt.Sprintf("%f", creditFloat*rates.Buy)
	kshFormattedCredit, _ := store.TruncateDecimalString(creditksh, 0)

	debtFloat, _ := strconv.ParseFloat(scaledDebt, 64)
	debtksh := fmt.Sprintf("%f", debtFloat*rates.Buy)
	kshFormattedDebt, _ := store.TruncateDecimalString(debtksh, 0)

	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_POOL_TO_SYMBOLS:   data.Symbols,
		storedb.DATA_POOL_TO_BALANCES:  data.Balances,
		storedb.DATA_POOL_TO_DECIMALS:  data.Decimals,
		storedb.DATA_POOL_TO_ADDRESSES: data.Addresses,
	}

	// Write data entries
	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	res.Content = l.Get("Credit: %s KSH\nDebt: %s KSH\n", kshFormattedCredit, kshFormattedDebt)

	return res, nil
}
