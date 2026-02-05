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

// CalculateMaxPayDebt calculates the max debt removal
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
	if inputStr == "0" || inputStr == "9" {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)
		return res, nil
	}

	userStore := h.userdataStore

	// Resolve active pool
	_, activePoolName, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)
		return res, err
	}

	metadata, err := store.GetSwapToVoucherData(ctx, userStore, sessionId, "1")
	if err != nil {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error)
		return res, fmt.Errorf("failed to retrieve swap to voucher data: %v", err)
	}
	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
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

	// call the api using the ActivePoolAddress, ActiveSwapToAddress as the from (FT), ActiveSwapFromAddress as the to (AT) and PublicKey to get the swap max limit
	r, err := h.accountService.GetSwapFromTokenMaxLimit(ctx, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress, swapData.ActiveSwapFromAddress, swapData.PublicKey)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on GetSwapFromTokenMaxLimit", "error", err)
		return res, nil
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(r.Max))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write full max amount entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", r.Max, "error", err)
		return res, err
	}

	// Scale down the amount
	maxAmountStr := store.ScaleDownBalance(r.Max, swapData.ActiveSwapToDecimal)
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

	res.Content = l.Get(
		"You can remove a maximum of %s %s from '%s'\n\nEnter amount of %s:",
		maxStr,
		swapData.ActiveSwapToSym,
		string(activePoolName),
		swapData.ActiveSwapToSym,
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

	storedMax, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)

	var finalAmountStr string
	if inputStr == swapData.ActiveSwapMaxAmount {
		finalAmountStr = string(storedMax)
	} else {
		finalAmountStr, err = store.ParseAndScaleAmount(inputStr, swapData.ActiveSwapToDecimal)
		if err != nil {
			return res, err
		}
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT, []byte(finalAmountStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", finalAmountStr, "error", err)
		return res, err
	}
	// store the user's input amount in the temporary value
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write inputStr amount entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", inputStr, "error", err)
		return res, err
	}

	// call the API to get the quote
	r, err := h.accountService.GetPoolSwapQuote(ctx, finalAmountStr, swapData.PublicKey, swapData.ActiveSwapToAddress, swapData.ActivePoolAddress, swapData.ActiveSwapFromAddress)
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

	res.Content = l.Get(
		"Please confirm that you will use %s %s to remove your debt of %s %s\n",
		inputStr, swapData.ActiveSwapToSym, qouteStr, swapData.ActiveSwapFromSym,
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

	// Resolve active pool
	_, activePoolName, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
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
	r, err := h.accountService.PoolSwap(ctx, swapAmountStr, swapData.PublicKey, swapData.ActiveSwapToAddress, swapData.ActivePoolAddress, swapData.ActiveSwapFromAddress)
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
		swapData.ActiveSwapToSym,
		activePoolName,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

func isStableVoucher(tokenAddress string) bool {
	addr := strings.ToLower(strings.TrimSpace(tokenAddress))
	for _, stable := range config.StableVoucherAddresses() {
		if addr == stable {
			return true
		}
	}
	return false
}
