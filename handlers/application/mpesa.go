package application

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/common/hex"
	"git.grassecon.net/grassrootseconomics/common/phone"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// GetMpesaMaxLimit returns the max FROM token
// check if max/tokenDecimals > 0.1 for UX purposes and to prevent swapping of dust values
func (h *MenuHandlers) GetMpesaMaxLimit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
	flag_low_swap_amount, _ := h.flagManager.GetFlag("flag_low_swap_amount")
	flag_incorrect_pool, _ := h.flagManager.GetFlag("flag_incorrect_pool")
	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "88" || inputStr == "98" {
		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error, flag_incorrect_voucher, flag_incorrect_pool)
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

	// Store the active transaction voucher data (from token)
	if err := store.StoreTransactionVoucher(ctx, h.userdataStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on StoreTransactionVoucher", "error", err)
		return res, err
	}

	// Fetch session data
	_, _, _, _, publicKey, _, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// call the mpesa rates API to get the rates
	rates, err := h.accountService.GetMpesaOnrampRates(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on GetMpesaOnrampRates", "error", err)
		return res, nil
	}

	txType := "swap"
	mpesaAddress := config.DefaultMpesaAddress()

	// Normalize the mpesa address to fetch the phone number
	publicKeyNormalized, err := hex.NormalizeHex(mpesaAddress)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to normalize alias address", "address", mpesaAddress, "error", err)
		return res, err
	}

	// get the recipient's phone number from the address
	recipientPhoneNumber, err := userStore.ReadEntry(ctx, publicKeyNormalized, storedb.DATA_PUBLIC_KEY_REVERSE)
	if err != nil || len(recipientPhoneNumber) == 0 {
		logg.WarnCtxf(ctx, "Alias address not registered, switching to normal transaction", "address", mpesaAddress)
		recipientPhoneNumber = nil
	}
	// store it for future reference (TODO)
	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER, recipientPhoneNumber); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write recipient phone number", "value", string(recipientPhoneNumber), "error", err)
		return res, err
	}

	// fetch data for verification (to_voucher data)
	recipientActiveSym, recipientActiveAddress, recipientActiveDecimal, err := h.getRecipientData(ctx, string(recipientPhoneNumber))
	if err != nil {
		return res, err
	}

	// Fetch min withdrawal amount from config/env
	minWithdraw := config.MinMpesaWithdrawAmount() // float64 (20)
	minKshFormatted, _ := store.TruncateDecimalString(fmt.Sprintf("%f", minWithdraw), 0)

	// If SAT is the same as RAT (default USDm),
	// or if the voucher is a stable coin
	// return early with KSH format
	if string(metadata.TokenAddress) == string(recipientActiveAddress) || isStableVoucher(metadata.TokenAddress) {
		txType = "normal"
		// Save the transaction type
		if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(txType)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write transaction type", "type", txType, "error", err)
			return res, err
		}

		activeFloat, _ := strconv.ParseFloat(string(metadata.Balance), 64)
		kshValue := activeFloat * rates.Buy

		maxKshFormatted, _ := store.TruncateDecimalString(fmt.Sprintf("%f", kshValue), 0)

		// Ensure that the max is greater than the min
		if kshValue < minWithdraw {
			res.FlagSet = append(res.FlagSet, flag_low_swap_amount)
			res.Content = l.Get("%s Ksh", maxKshFormatted)
			return res, nil
		}

		res.Content = l.Get(
			"Enter the amount of Mpesa to withdraw: (Min: Ksh %s, Max %s Ksh)\n",
			minKshFormatted,
			maxKshFormatted,
		)

		res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error, flag_incorrect_voucher, flag_incorrect_pool)

		return res, nil
	}

	// Resolve active pool address
	activePoolAddress, _, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// Check if selected token is swappable
	canSwap, err := h.accountService.CheckTokenInPool(ctx, string(activePoolAddress), string(metadata.TokenAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on CheckTokenInPool", "error", err)
		return res, nil
	}

	if !canSwap.CanSwapFrom { // pool issue (CATCH on .vis)
		res.FlagSet = append(res.FlagSet, flag_incorrect_pool)
		res.Content = "0"
		return res, nil
	}

	// retrieve the max credit send amounts
	_, maxRAT, err := h.calculateSendCreditLimits(ctx, activePoolAddress, []byte(metadata.TokenAddress), recipientActiveAddress, publicKey, []byte(metadata.TokenDecimals), recipientActiveDecimal)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = "0"
		logg.ErrorCtxf(ctx, "failed on calculateSendCreditLimits", "error", err)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_api_call_error)

	// Fallback if below minimum
	maxFloat, _ := strconv.ParseFloat(maxRAT, 64)
	if maxFloat < 0.1 {
		formatted, _ := store.TruncateDecimalString(maxRAT, 2)
		res.Content = formatted
		res.FlagSet = append(res.FlagSet, flag_low_swap_amount)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_low_swap_amount)

	// Save max RAT amount to be used in validating the user's input
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, []byte(maxRAT))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap max amount (maxRAT)", "value", maxRAT, "error", err)
		return res, err
	}

	// Save the transaction type
	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(txType)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write transaction type", "type", txType, "error", err)
		return res, err
	}

	// save swap related data for the swap preview (the swap to)
	swapMetadata := &dataserviceapi.TokenHoldings{
		TokenAddress:  string(recipientActiveAddress),
		TokenSymbol:   string(recipientActiveSym),
		TokenDecimals: string(recipientActiveDecimal),
	}

	// Store the active swap_to data
	if err := store.UpdateSwapToVoucherData(ctx, userStore, sessionId, swapMetadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateSwapToVoucherData", "error", err)
		return res, err
	}

	maxKsh := maxFloat * rates.Buy
	kshStr := fmt.Sprintf("%f", maxKsh)
	maxKshFormatted, _ := store.TruncateDecimalString(kshStr, 0)

	res.Content = l.Get(
		"Enter the amount of Mpesa to withdraw: (Min: Ksh %s, Max %s Ksh)\n",
		minKshFormatted,
		maxKshFormatted,
	)

	res.FlagReset = append(res.FlagReset, flag_low_swap_amount, flag_api_call_error, flag_incorrect_voucher, flag_incorrect_pool)

	return res, nil
}

// GetMpesaPreview displays the get mpesa preview and estimates
func (h *MenuHandlers) GetMpesaPreview(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// INPUT IN RAT Ksh
	inputStr := string(input)
	if inputStr == "0" || inputStr == "9" {
		return res, nil
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	// call the mpesa rates API to get the rates
	rates, err := h.accountService.GetMpesaOnrampRates(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on GetMpesaOnrampRates", "error", err)
		return res, nil
	}

	// Input in Ksh
	kshAmount, err := strconv.ParseFloat(inputStr, 64)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	min := config.MinMpesaWithdrawAmount()

	if kshAmount < min {
		// if the input is below the minimum
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	// divide by the buy rate
	inputAmount := kshAmount / rates.Buy

	// Resolve active pool
	activePoolAddress, _, err := h.resolveActivePoolDetails(ctx, sessionId)
	if err != nil {
		return res, err
	}

	transactionType, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE)
	if err != nil {
		return res, err
	}

	// get the selected voucher
	mpesaWithdrawalVoucher, err := store.GetTransactionVoucherData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTransactionVoucherData", "error", err)
		return res, err
	}

	if string(transactionType) == "normal" {
		// get the max based on the selected voucher balance
		maxValue, err := strconv.ParseFloat(mpesaWithdrawalVoucher.Balance, 64)
		if err != nil {
			logg.ErrorCtxf(ctx, "Failed to convert the stored balance string to a float", "error", err)
			return res, err
		}
		if inputAmount > maxValue {
			res.FlagSet = append(res.FlagSet, flag_invalid_amount)
			res.Content = inputStr
			return res, nil
		}

		// Format the input amount to 2 decimal places
		inputAmountStr := fmt.Sprintf("%f", inputAmount)
		qouteInputAmount, _ := store.TruncateDecimalString(inputAmountStr, 2)

		// store the inputAmountStr as the final amount (that will be sent)
		err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(inputAmountStr))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write send amount value entry with", "key", storedb.DATA_AMOUNT, "value", inputAmountStr, "error", err)
			return res, err
		}

		res.Content = l.Get(
			"You are sending %s %s in order to receive ~ %s ksh",
			qouteInputAmount, mpesaWithdrawalVoucher.TokenSymbol, inputStr,
		)

		return res, nil
	}

	swapToVoucher, err := store.ReadSwapToVoucher(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on ReadSwapFromVoucher", "error", err)
		return res, err
	}

	swapMaxAmount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read swapMaxAmount entry with", "key", storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, "error", err)
		return res, err
	}

	// use the stored max RAT
	maxRATValue, err := strconv.ParseFloat(string(swapMaxAmount), 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the swapMaxAmount to a float", "error", err)
		return res, err
	}

	if inputAmount > maxRATValue {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	// Format the amount to 2 decimal places
	formattedAmount, err := store.TruncateDecimalString(fmt.Sprintf("%f", inputAmount), 2)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	finalAmountStr, err := store.ParseAndScaleAmount(formattedAmount, swapToVoucher.TokenDecimals)
	if err != nil {
		return res, err
	}

	// call the credit send API to get the reverse quote
	r, err := h.accountService.GetCreditSendReverseQuote(ctx, string(activePoolAddress), mpesaWithdrawalVoucher.TokenAddress, swapToVoucher.TokenAddress, finalAmountStr)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed GetCreditSendReverseQuote poolSwap", "error", err)
		return res, nil
	}

	sendInputAmount := r.InputAmount   // amount of SAT
	sendOutputAmount := r.OutputAmount // amount of RAT

	// store the sendOutputAmount as the final amount (that will be sent)
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(sendOutputAmount))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write output amount value entry with", "key", storedb.DATA_AMOUNT, "value", sendOutputAmount, "error", err)
		return res, err
	}

	// store the sendInputAmount as the swap amount
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT, []byte(sendInputAmount))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap amount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "value", sendInputAmount, "error", err)
		return res, err
	}

	// covert for display
	quoteInputStr := store.ScaleDownBalance(sendInputAmount, mpesaWithdrawalVoucher.TokenDecimals)
	// Format the quoteInputStr amount to 2 decimal places
	qouteInputAmount, _ := store.TruncateDecimalString(quoteInputStr, 2)

	res.Content = l.Get(
		"You are sending %s %s in order to receive ~ %s ksh",
		qouteInputAmount, mpesaWithdrawalVoucher.TokenSymbol, inputStr,
	)

	return res, nil
}

// InitiateGetMpesa calls the poolSwap, followed by the transfer and returns a confirmation based on the result.
func (h *MenuHandlers) InitiateGetMpesa(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")
	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	mpesaAddress := config.DefaultMpesaAddress()

	transactionType, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE)
	if err != nil {
		return res, err
	}

	// get the selected voucher
	mpesaWithdrawalVoucher, err := store.GetTransactionVoucherData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTransactionVoucherData", "error", err)
		return res, err
	}

	if string(transactionType) == "normal" {
		// Call TokenTransfer for the normal transaction
		data, err := store.ReadTransactionData(ctx, h.userdataStore, sessionId)
		if err != nil {
			return res, err
		}

		finalAmountStr, err := store.ParseAndScaleAmount(data.Amount, mpesaWithdrawalVoucher.TokenDecimals)
		if err != nil {
			return res, err
		}

		tokenTransfer, err := h.accountService.TokenTransfer(ctx, finalAmountStr, data.PublicKey, mpesaAddress, mpesaWithdrawalVoucher.TokenAddress)
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_call_error)
			res.Content = l.Get("Your request failed. Please try again later.")
			logg.ErrorCtxf(ctx, "failed on TokenTransfer", "error", err)
			return res, nil
		}

		logg.InfoCtxf(ctx, "TokenTransfer normal", "trackingId", tokenTransfer.TrackingId)

		res.Content = l.Get("Your request has been sent. Please await confirmation")

		res.FlagReset = append(res.FlagReset, flag_account_authorized)
		return res, nil
	}

	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	swapToVoucher, err := store.ReadSwapToVoucher(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on ReadSwapFromVoucher", "error", err)
		return res, err
	}

	// Resolve active pool
	activePoolAddress, _, err := h.resolveActivePoolDetails(ctx, sessionId)
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
	poolSwap, err := h.accountService.PoolSwap(ctx, swapAmountStr, string(publicKey), mpesaWithdrawalVoucher.TokenAddress, string(activePoolAddress), swapToVoucher.TokenAddress)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	logg.InfoCtxf(ctx, "mpesa poolSwap before transfer", "swapTrackingId", poolSwap.TrackingId)

	// TODO: remove this temporary time delay and replace with a swap and send endpoint
	time.Sleep(1 * time.Second)

	amount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)
	if err != nil {
		return res, err
	}

	// Initiate a send to mpesa after the swap
	tokenTransfer, err := h.accountService.TokenTransfer(ctx, string(amount), string(publicKey), mpesaAddress, swapToVoucher.TokenAddress)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on TokenTransfer after swap", "error", err)
		return res, nil
	}

	logg.InfoCtxf(ctx, "final TokenTransfer after swap", "trackingId", tokenTransfer.TrackingId)

	res.Content = l.Get("Your request has been sent. Please await confirmation")
	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

// SendMpesaMinLimit returns the min amount from the config
func (h *MenuHandlers) SendMpesaMinLimit(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "9" {
		return res, nil
	}

	// Fetch min amount from config/env
	min := config.MinMpesaSendAmount()

	// Convert to string
	ksh := fmt.Sprintf("%f", min)

	// Format (e.g., 100.0 -> 100)
	kshFormatted, _ := store.TruncateDecimalString(ksh, 0)

	res.Content = l.Get(
		"Enter the amount of credit to deposit: (Minimum %s Ksh)\n",
		kshFormatted,
	)

	return res, nil
}

// SendMpesaPreview displays the send mpesa preview and estimates
func (h *MenuHandlers) SendMpesaPreview(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// INPUT IN Ksh
	inputStr := string(input)
	if inputStr == "0" || inputStr == "9" {
		return res, nil
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	// call the mpesa rates API to get the rates
	rates, err := h.accountService.GetMpesaOnrampRates(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on GetMpesaOnrampRates", "error", err)
		return res, nil
	}

	// Input in Ksh
	kshAmount, err := strconv.ParseFloat(inputStr, 64)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	min := config.MinMpesaSendAmount()
	max := config.MaxMpesaSendAmount()

	if kshAmount > max || kshAmount < min {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_invalid_amount)

	// store the user's raw input amount
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write amount inputStr entry with", "key", storedb.DATA_AMOUNT, "value", inputStr, "error", err)
		return res, err
	}

	estimateValue := kshAmount / rates.Sell
	estimateStr := fmt.Sprintf("%f", estimateValue)
	estimateFormatted, _ := store.TruncateDecimalString(estimateStr, 2)

	defaultAsset := config.DefaultMpesaAsset()

	res.Content = l.Get(
		"You will get a prompt for your Mpesa PIN shortly to send %s ksh and receive ~ %s %s",
		inputStr, estimateFormatted, defaultAsset,
	)

	return res, nil
}

// InitiateSendMpesa calls the trigger-onram API to initiate the purchase
func (h *MenuHandlers) InitiateSendMpesa(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")
	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	phoneNumber, err := phone.FormatToLocalPhoneNumber(sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on FormatToLocalPhoneNumber", "session-id", sessionId, "error", err)
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		return res, nil
	}

	defaultAsset := config.DefaultMpesaAsset()

	amount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read amount entry", "key", storedb.DATA_AMOUNT, "error", err)
		return res, err
	}

	amountInt, err := strconv.Atoi(string(amount))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to convert amount to int", "amount", string(amount), "error", err)
		return res, err
	}

	// Call the trigger onramp API
	triggerOnramp, err := h.accountService.MpesaTriggerOnramp(ctx, string(publicKey), phoneNumber, defaultAsset, amountInt)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on MpesaTriggerOnramp", "error", err)
		return res, nil
	}

	logg.InfoCtxf(ctx, "MpesaTriggerOnramp", "transactionCode", triggerOnramp.TransactionCode)

	res.Content = l.Get("Your request has been sent. Thank you for using Sarafu")
	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}
