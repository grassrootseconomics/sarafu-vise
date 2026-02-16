package application

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
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

	// default response
	res.FlagReset = append(res.FlagReset, flag_api_call_error)
	res.Content = l.Get("Credit: %s KSH\nDebt: %s KSH\n", "0", "0")

	// Fetch session data
	_, activeBal, activeSym, activeAddress, publicKey, activeDecimal, err := h.getSessionData(ctx, sessionId)
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

	// Build stable voucher priority (lower index = higher priority)
	stablePriority := make(map[string]int)
	stableAddresses := config.StableVoucherAddresses()
	for i, addr := range stableAddresses {
		stablePriority[addr] = i
	}

	stable := make([]dataserviceapi.TokenHoldings, 0)
	nonStable := make([]dataserviceapi.TokenHoldings, 0)

	// Helper: order vouchers (stable first, priority-based)
	orderVouchers := func(vouchers []dataserviceapi.TokenHoldings) []dataserviceapi.TokenHoldings {
		for _, v := range vouchers {
			if isStableVoucher(v.TokenAddress) {
				stable = append(stable, v)
			} else {
				nonStable = append(nonStable, v)
			}
		}

		sort.SliceStable(stable, func(i, j int) bool {
			ai := stablePriority[stable[i].TokenAddress]
			aj := stablePriority[stable[j].TokenAddress]
			return ai < aj
		})

		return append(stable, nonStable...)
	}

	// Remove active voucher
	filteredVouchers := make([]dataserviceapi.TokenHoldings, 0, len(swappableVouchers))
	for _, v := range swappableVouchers {
		if v.TokenSymbol != string(activeSym) {
			filteredVouchers = append(filteredVouchers, v)
		}
	}

	// Order remaining vouchers
	orderedFilteredVouchers := orderVouchers(filteredVouchers)

	// Process & store
	data := store.ProcessVouchers(orderedFilteredVouchers)

	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_VOUCHER_SYMBOLS:   data.Symbols,
		storedb.DATA_VOUCHER_BALANCES:  data.Balances,
		storedb.DATA_VOUCHER_DECIMALS:  data.Decimals,
		storedb.DATA_VOUCHER_ADDRESSES: data.Addresses,
	}

	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	// Credit calculation: How much Active Token (such as ALF) that can be swapped for a stable coin
	// + any stables sendable to Pretium (in KSH value)
	scaledCredit := "0"

	finalAmountStr, err := store.ParseAndScaleAmount(string(activeBal), string(activeDecimal))
	if err != nil {
		return res, err
	}
	// do a swap quote to get the max I can get when I swap my active voucher
	// for a stable coin (say I can get 4 USD). Then I add that to my exisitng
	// stable coins and covert to Ksh
	stableAddress := stableAddresses[0]
	r, err := h.accountService.GetPoolSwapQuote(ctx, finalAmountStr, string(publicKey), string(activeAddress), string(activePoolAddress), stableAddress)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	finalQuote := store.ScaleDownBalance(r.OutValue, "6")

	scaledCredit = store.AddDecimalStrings(scaledCredit, finalQuote)

	for _, v := range stable {
		scaled := store.ScaleDownBalance(v.Balance, v.TokenDecimals)
		scaledCredit = store.AddDecimalStrings(scaledCredit, scaled)
	}

	// DEBT calculation: All outstanding active token that is in the current pool
	// (how much of AT that is in the active Pool)
	scaledDebt := "0"
	// convert the current balance to Ksh
	scaledDebt = string(activeBal)

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

	res.Content = l.Get(
		"Credit: %s KSH\nDebt: %s KSH\n",
		kshFormattedCredit,
		kshFormattedDebt,
	)

	return res, nil
}
