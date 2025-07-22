package application

import (
	"context"
	"fmt"
	"strconv"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// LoadSwapFromList returns a list of possible vouchers to swap to
func (h *MenuHandlers) LoadSwapToList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore

	// get the active address and symbol
	activeAddress, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeAddress entry with", "key", storedb.DATA_ACTIVE_ADDRESS, "error", err)
		return res, err
	}
	activeSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
		return res, err
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")

	inputStr := string(input)
	if inputStr == "0" {
		return res, nil
	}

	// Get active pool address and symbol or fall back to default
	var activePoolAddress []byte
	activePoolAddress, err = userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_ADDRESS)
	if err != nil {
		if db.IsNotFound(err) {
			defaultPoolAddress := config.DefaultPoolAddress()
			// store the default as the active pool address
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_ADDRESS, []byte(defaultPoolAddress))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write default PoolContractAdrress", "key", storedb.DATA_ACTIVE_POOL_ADDRESS, "value", defaultPoolAddress, "error", err)
				return res, err
			}
			activePoolAddress = []byte(defaultPoolAddress)
		} else {
			logg.ErrorCtxf(ctx, "failed to read active PoolContractAdrress", "key", storedb.DATA_ACTIVE_POOL_ADDRESS, "error", err)
			return res, err
		}
	}

	var activePoolSymbol []byte
	activePoolSymbol, err = userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM)
	if err != nil {
		if db.IsNotFound(err) {
			defaultPoolSym := config.DefaultPoolName()
			// store the default as the active pool symbol
			err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM, []byte(defaultPoolSym))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write default Pool Symbol", "key", storedb.DATA_ACTIVE_POOL_SYM, "value", defaultPoolSym, "error", err)
				return res, err
			}
			activePoolSymbol = []byte(defaultPoolSym)
		} else {
			logg.ErrorCtxf(ctx, "failed to read active Pool symbol", "key", storedb.DATA_ACTIVE_POOL_SYM, "error", err)
			return res, err
		}
	}

	// call the api using the ActivePoolAddress and ActiveVoucherAddress to check if it is part of the pool
	r, err := h.accountService.CheckTokenInPool(ctx, string(activePoolAddress), string(activeAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on CheckTokenInPool", "error", err)
		return res, err
	}

	logg.InfoCtxf(ctx, "CheckTokenInPool", "response", r, "active_pool_address", string(activePoolAddress), "active_symbol_address", string(activeAddress))

	if !r.CanSwapFrom {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		res.Content = l.Get(
			"%s is not in %s. Please update your voucher and try again.",
			activeSym,
			activePoolSymbol,
		)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)

	// call the api using the activePoolAddress to get a list of SwapToSymbolsData
	swapToList, err := h.accountService.GetPoolSwappableVouchers(ctx, string(activePoolAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on FetchTransactions", "error", err)
		return res, err
	}

	logg.InfoCtxf(ctx, "GetPoolSwappableVouchers", "swapToList", swapToList)

	// Return if there are no vouchers
	if len(swapToList) == 0 {
		return res, nil
	}

	// Filter out the active voucher from swapToList
	filteredSwapToList := make([]dataserviceapi.TokenHoldings, 0, len(swapToList))
	for _, s := range swapToList {
		if s.TokenSymbol != string(activeSym) {
			filteredSwapToList = append(filteredSwapToList, s)
		}
	}

	// Store filtered swap to list data (excluding the current active voucher)
	data := store.ProcessVouchers(filteredSwapToList)

	logg.InfoCtxf(ctx, "ProcessVouchers", "data", data)

	// Store all swap_to tokens data
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

	res.Content = h.ReplaceSeparatorFunc(data.Symbols)

	return res, nil
}

// SwapMaxLimit returns the max FROM token
// check if max/tokenDecimals > 0.1 for UX purposes and to prevent swapping of dust values
func (h *MenuHandlers) SwapMaxLimit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")
	flag_low_swap_amount, _ := h.flagManager.GetFlag("flag_low_swap_amount")

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher, flag_low_swap_amount)

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "88" || inputStr == "98" {
		return res, nil
	}

	userStore := h.userdataStore
	metadata, err := store.GetSwapToVoucherData(ctx, userStore, sessionId, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve swap to voucher data: %v", err)
	}
	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		return res, nil
	}

	logg.InfoCtxf(ctx, "Metadata from GetSwapToVoucherData:", "metadata", metadata)

	// Store the active swap_to data
	if err := store.UpdateSwapToVoucherData(ctx, userStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateSwapToVoucherData", "error", err)
		return res, err
	}

	swapData, err := store.ReadSwapData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	// call the api using the ActivePoolAddress, ActiveSwapFromAddress, ActiveSwapToAddress and PublicKey to get the swap max limit
	logg.InfoCtxf(ctx, "Call GetSwapFromTokenMaxLimit with:", "ActivePoolAddress", swapData.ActivePoolAddress, "ActiveSwapFromAddress", swapData.ActiveSwapFromAddress, "ActiveSwapToAddress", swapData.ActiveSwapToAddress, "publicKey", swapData.PublicKey)
	r, err := h.accountService.GetSwapFromTokenMaxLimit(ctx, swapData.ActivePoolAddress, swapData.ActiveSwapFromAddress, swapData.ActiveSwapToAddress, swapData.PublicKey)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on GetSwapFromTokenMaxLimit", "error", err)
		return res, err
	}

	// Scale down the amount
	maxAmountStr := store.ScaleDownBalance(r.Max, swapData.ActiveSwapFromDecimal)
	if err != nil {
		return res, err
	}

	maxAmountFloat, err := strconv.ParseFloat(maxAmountStr, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to parse maxAmountStr as float", "value", maxAmountStr, "error", err)
		return res, err
	}

	// Format to 2 decimal places
	maxStr := fmt.Sprintf("%.2f", maxAmountFloat)

	if maxAmountFloat < 0.1 {
		// return with low amount flag
		res.Content = maxStr
		res.FlagSet = append(res.FlagSet, flag_low_swap_amount)
		return res, nil
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, []byte(maxStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap max amount entry with", "key", storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, "value", maxStr, "error", err)
		return res, err
	}

	res.Content = fmt.Sprintf(
		"Maximum: %s\n\nEnter amount of %s to swap for %s:",
		maxStr, swapData.ActiveSwapFromSym, swapData.ActiveSwapToSym,
	)

	return res, nil
}

// SwapPreview displays the swap preview and estimates
func (h *MenuHandlers) SwapPreview(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	inputStr := string(input)
	if inputStr == "0" {
		return res, nil
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	maxValue, err := strconv.ParseFloat(swapData.ActiveSwapMaxAmount, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the swapMaxAmount to a float", "error", err)
		return res, err
	}

	inputAmount, err := strconv.ParseFloat(inputStr, 64)
	if err != nil || inputAmount > maxValue {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	// Format the amount to 2 decimal places
	formattedAmount, err := store.TruncateDecimalString(inputStr, 2)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	finalAmountStr, err := store.ParseAndScaleAmount(formattedAmount, swapData.ActiveSwapFromDecimal)
	if err != nil {
		return res, err
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT, []byte(finalAmountStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", finalAmountStr, "error", err)
		return res, err
	}
	// store the user's input amount in the temporary value
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", finalAmountStr, "error", err)
		return res, err
	}

	// call the API to get the quote
	r, err := h.accountService.GetPoolSwapQuote(ctx, finalAmountStr, swapData.PublicKey, swapData.ActiveSwapFromAddress, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	// Scale down the quoted amount
	quoteAmountStr := store.ScaleDownBalance(r.OutValue, swapData.ActiveSwapToDecimal)
	qouteAmount, err := strconv.ParseFloat(quoteAmountStr, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to parse quoteAmountStr as float", "value", quoteAmountStr, "error", err)
		return res, err
	}

	// Format to 2 decimal places
	qouteStr := fmt.Sprintf("%.2f", qouteAmount)

	res.Content = fmt.Sprintf(
		"You will swap:\n%s %s for %s %s:",
		inputStr, swapData.ActiveSwapFromSym, qouteStr, swapData.ActiveSwapToSym,
	)

	return res, nil
}

// InitiateSwap calls the poolSwap and returns a confirmation based on the result.
func (h *MenuHandlers) InitiateSwap(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	swapAmount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read swapAmount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "error", err)
		return res, err
	}

	swapAmountStr := string(swapAmount)

	// Call the poolSwap API
	r, err := h.accountService.PoolSwap(ctx, swapAmountStr, swapData.PublicKey, swapData.ActiveSwapFromAddress, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	trackingId := r.TrackingId
	logg.InfoCtxf(ctx, "poolSwap", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. You will receive an SMS when your %s %s has been swapped for %s.",
		swapData.TemporaryValue,
		swapData.ActiveSwapFromSym,
		swapData.ActiveSwapToSym,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}
