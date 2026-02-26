package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// GetOrderedVouchers returns a list of ordered vouchers with stables at the top
func (h *MenuHandlers) GetOrderedVouchers(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	// Read ordered vouchers from the store
	voucherData, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ORDERED_VOUCHER_SYMBOLS)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read stable voucherData entires with", "key", storedb.DATA_ORDERED_VOUCHER_SYMBOLS, "error", err)
		return res, err
	}

	if len(voucherData) == 0 {
		return res, nil
	}

	voucherBalances, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ORDERED_VOUCHER_BALANCES)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read stable voucherData entires with", "key", storedb.DATA_ORDERED_VOUCHER_BALANCES, "error", err)
		return res, err
	}

	formattedVoucherList := store.FormatVoucherList(ctx, string(voucherData), string(voucherBalances))
	finalOutput := strings.Join(formattedVoucherList, "\n")

	res.Content = l.Get("Select number or symbol from your vouchers:\n%s", finalOutput)

	return res, nil
}

// PoolDepositMaxAmount returns the balance of the selected voucher
func (h *MenuHandlers) PoolDepositMaxAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "88" || inputStr == "98" {
		return res, nil
	}

	userStore := h.userdataStore
	metadata, err := store.GetOrderedVoucherData(ctx, userStore, sessionId, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve swap to voucher data: %v", err)
	}
	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		return res, nil
	}

	// Store the pool deposit voucher data
	if err := store.StoreTransactionVoucher(ctx, h.userdataStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on StoreTransactionVoucher", "error", err)
		return res, err
	}

	// Format the balance amount to 2 decimal places
	formattedBalance, _ := store.TruncateDecimalString(string(metadata.Balance), 2)

	res.Content = l.Get("Maximum amount: %s %s\nEnter amount:", formattedBalance, metadata.TokenSymbol)

	return res, nil
}

// ConfirmPoolDeposit displays the pool deposit preview for a PIN confirmation
func (h *MenuHandlers) ConfirmPoolDeposit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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

	res.FlagReset = append(res.FlagReset, flag_invalid_amount)

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	poolDepositVoucher, err := store.GetTransactionVoucherData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTransactionVoucherData", "error", err)
		return res, err
	}

	maxValue, err := strconv.ParseFloat(poolDepositVoucher.Balance, 64)
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

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write pool deposit amount entry with", "key", storedb.DATA_AMOUNT, "value", inputStr, "error", err)
		return res, err
	}

	// Resolve active pool
	_, activePoolSymbol, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	res.Content = l.Get(
		"You will deposit %s %s into %s\n",
		inputStr, poolDepositVoucher.TokenSymbol, activePoolSymbol,
	)

	return res, nil
}

// InitiatePoolDeposit calls the pool deposit API
func (h *MenuHandlers) InitiatePoolDeposit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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
	activePoolAddress, activePoolSymbol, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on resolveActivePoolDetails", "error", err)
		return res, err
	}

	poolDepositVoucher, err := store.GetTransactionVoucherData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTransactionVoucherData", "error", err)
		return res, err
	}

	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	amount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read amount entry", "key", storedb.DATA_AMOUNT, "error", err)
		return res, err
	}

	finalAmountStr, err := store.ParseAndScaleAmount(string(amount), poolDepositVoucher.TokenDecimals)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on ParseAndScaleAmount", "error", err)
		return res, err
	}

	// Call token transfer API and send the token to the pool address
	r, err := h.accountService.TokenTransfer(ctx, finalAmountStr, string(publicKey), string(activePoolAddress), poolDepositVoucher.TokenAddress)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on pool deposit", "error", err)
		return res, nil
	}

	trackingId := r.TrackingId
	logg.InfoCtxf(ctx, "Pool deposit", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. You will receive an SMS when %s %s has been deposited into %s.",
		string(amount),
		poolDepositVoucher.TokenSymbol,
		activePoolSymbol,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}
