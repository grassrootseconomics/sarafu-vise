package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// CalculateMaxPayDebt calculates the max debt removal based on the selected voucher
func (h *MenuHandlers) CalculateMaxPayDebt(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
	flag_low_swap_amount, _ := h.flagManager.GetFlag("flag_low_swap_amount")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "88" || inputStr == "98" {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)
		return res, nil
	}

	userStore := h.userdataStore

	// Fetch session data
	_, _, activeSym, activeAddress, publicKey, activeDecimal, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, nil
	}

	// Resolve active pool
	activePoolAddress, activePoolName, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)
		return res, err
	}

	// get the voucher data based on the input
	metadata, err := store.GetVoucherData(ctx, userStore, sessionId, inputStr)
	if err != nil {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)
		return res, fmt.Errorf("failed to retrieve swap to voucher data: %v", err)
	}
	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		return res, nil
	}

	// Get the max swap limit with the selected voucher
	r, err := h.accountService.GetSwapFromTokenMaxLimit(ctx, string(activePoolAddress), metadata.TokenAddress, string(activeAddress), string(publicKey))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on GetSwapFromTokenMaxLimit", "error", err)
		return res, nil
	}

	maxLimit := r.Max

	metadata.Balance = maxLimit

	// Store the active swap from data
	if err := store.UpdateSwapFromVoucherData(ctx, userStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateSwapFromVoucherData", "error", err)
		return res, err
	}

	// Scale down the amount
	maxAmountStr := store.ScaleDownBalance(maxLimit, metadata.TokenDecimals)
	if err != nil {
		return res, err
	}

	maxAmountFloat, err := strconv.ParseFloat(maxAmountStr, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to parse maxAmountStr as float", "value", maxAmountStr, "error", err)
		return res, err
	}

	// Format to 2 decimal places
	maxStr, _ := store.TruncateDecimalString(string(maxAmountStr), 2)

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

	// Do a pool quote to get the max AT that can be removed (gotten)
	// if we swapped the max of the FT

	// call the API to get the quote
	qoute, err := h.accountService.GetPoolSwapQuote(ctx, maxLimit, string(publicKey), metadata.TokenAddress, string(activePoolAddress), string(activeAddress))
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	// Scale down the quoted amount
	quoteAmountStr := store.ScaleDownBalance(qoute.OutValue, string(activeDecimal))

	// Format to 2 decimal places
	quoteStr, _ := store.TruncateDecimalString(string(quoteAmountStr), 2)

	res.Content = l.Get(
		"You can remove a max of %s %s from '%s'\nEnter amount of %s:(Max: %s)",
		quoteStr,
		string(activeSym),
		string(activePoolName),
		metadata.TokenSymbol,
		maxStr,
	)

	res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)

	return res, nil
}

// ConfirmDebtRemoval displays the debt preview for a confirmation
func (h *MenuHandlers) ConfirmDebtRemoval(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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

	// Fetch session data
	_, _, activeSym, activeAddress, publicKey, _, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, nil
	}

	payDebtVoucher, err := store.ReadSwapFromVoucher(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on ReadSwapFromVoucher", "error", err)
		return res, err
	}

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
	if err != nil || inputAmount > maxValue || inputAmount < 0.1 {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	var finalAmountStr string
	if inputStr == swapData.ActiveSwapMaxAmount {
		finalAmountStr = string(payDebtVoucher.Balance)
	} else {
		finalAmountStr, err = store.ParseAndScaleAmount(inputStr, payDebtVoucher.TokenDecimals)
		if err != nil {
			return res, err
		}
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT, []byte(finalAmountStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", finalAmountStr, "error", err)
		return res, err
	}

	// call the API to get the quote
	r, err := h.accountService.GetPoolSwapQuote(ctx, finalAmountStr, string(publicKey), payDebtVoucher.TokenAddress, swapData.ActivePoolAddress, string(activeAddress))
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	// Scale down the quoted amount
	quoteAmountStr := store.ScaleDownBalance(r.OutValue, swapData.ActiveSwapFromDecimal)

	// Format to 2 decimal places
	qouteStr, _ := store.TruncateDecimalString(string(quoteAmountStr), 2)

	// store the quote in the temporary value key
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(qouteStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap max amount entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", qouteStr, "error", err)
		return res, err
	}

	res.Content = l.Get(
		"Please confirm that you will use %s %s to remove your debt of %s %s\n",
		inputStr, payDebtVoucher.TokenSymbol, qouteStr, string(activeSym),
	)

	return res, nil
}

// InitiatePayDebt calls the poolSwap to swap the token for the active voucher.
func (h *MenuHandlers) InitiatePayDebt(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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

	// Fetch session data
	_, _, activeSym, activeAddress, publicKey, _, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, nil
	}

	// Resolve active pool
	activePoolAddress, activePoolName, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	payDebtVoucher, err := store.ReadSwapFromVoucher(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on ReadSwapFromVoucher", "error", err)
		return res, err
	}

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
	r, err := h.accountService.PoolSwap(ctx, swapAmountStr, string(publicKey), payDebtVoucher.TokenAddress, string(activePoolAddress), string(activeAddress))
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	trackingId := r.TrackingId
	logg.InfoCtxf(ctx, "poolSwap", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. You will receive an SMS when your debt of %s %s has been removed from %s.",
		swapData.TemporaryValue,
		string(activeSym),
		activePoolName,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

func isStableVoucher(tokenAddress string) bool {
	addr := strings.TrimSpace(tokenAddress)
	for _, stable := range config.StableVoucherAddresses() {
		if addr == stable {
			return true
		}
	}
	return false
}
