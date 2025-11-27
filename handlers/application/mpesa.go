package application

import (
	"context"
	"fmt"
	"strconv"

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

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "9" {
		return res, nil
	}

	userStore := h.userdataStore

	// Fetch session data
	_, activeBal, _, activeAddress, publicKey, activeDecimal, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, err
	}

	rate := config.MpesaRate()
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

	// fetch data for verification
	recipientActiveSym, recipientActiveAddress, recipientActiveDecimal, err := h.getRecipientData(ctx, string(recipientPhoneNumber))
	if err != nil {
		return res, err
	}

	// If RAT is the same as SAT, return early with KSH format
	if string(activeAddress) == string(recipientActiveAddress) {
		txType = "normal"
		// Save the transaction type
		if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(txType)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write transaction type", "type", txType, "error", err)
			return res, err
		}

		activeFloat, _ := strconv.ParseFloat(string(activeBal), 64)
		ksh := fmt.Sprintf("%f", activeFloat*rate)

		kshFormatted, _ := store.TruncateDecimalString(ksh, 0)

		res.Content = l.Get(
			"Enter the amount of Mpesa to get: (Max %s Ksh)\n",
			kshFormatted,
		)

		return res, nil
	}

	// Resolve active pool address
	activePoolAddress, err := h.resolveActivePoolAddress(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// Check if sender token is swappable
	canSwap, err := h.accountService.CheckTokenInPool(ctx, string(activePoolAddress), string(activeAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on CheckTokenInPool", "error", err)
		return res, nil
	}

	if !canSwap.CanSwapFrom { // pool issue (TODO on vis)
		res.FlagSet = append(res.FlagSet, flag_incorrect_pool)
		return res, nil
	}

	// retrieve the max credit send amounts
	_, maxRAT, err := h.calculateSendCreditLimits(ctx, activePoolAddress, activeAddress, recipientActiveAddress, publicKey, activeDecimal, recipientActiveDecimal)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on calculateSendCreditLimits", "error", err)
		return res, nil
	}

	// Fallback if below minimum
	maxFloat, _ := strconv.ParseFloat(maxRAT, 64)
	if maxFloat < 0.1 {
		formatted, _ := store.TruncateDecimalString(maxRAT, 2)
		res.Content = formatted
		res.FlagSet = append(res.FlagSet, flag_low_swap_amount)
		return res, nil
	}

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

	// save swap related data for the swap preview
	metadata := &dataserviceapi.TokenHoldings{
		TokenAddress:  string(recipientActiveAddress),
		TokenSymbol:   string(recipientActiveSym),
		TokenDecimals: string(recipientActiveDecimal),
	}

	// Store the active swap_to data
	if err := store.UpdateSwapToVoucherData(ctx, userStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateSwapToVoucherData", "error", err)
		return res, err
	}

	maxKsh := maxFloat * rate
	kshStr := fmt.Sprintf("%f", maxKsh)
	kshFormatted, _ := store.TruncateDecimalString(kshStr, 0)

	res.Content = l.Get(
		"Enter the amount of Mpesa to get: (Max %s Ksh)\n",
		kshFormatted,
	)

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
	if inputStr == "9" {
		return res, nil
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore
	rate := config.MpesaRate()

	// Input in Ksh
	kshAmount, err := strconv.ParseFloat(inputStr, 64)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	// divide by the rate
	inputAmount := kshAmount / rate

	// store the user's raw input amount in the temporary value
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write temporary inputStr entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", inputStr, "error", err)
		return res, err
	}

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	transactionType, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE)
	if err != nil {
		return res, err
	}

	if string(transactionType) == "normal" {
		activeBal, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
			return res, err
		}
		balanceValue, err := strconv.ParseFloat(string(activeBal), 64)
		if err != nil {
			logg.ErrorCtxf(ctx, "Failed to convert the activeBal to a float", "error", err)
			return res, err
		}

		if inputAmount > balanceValue {
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
			"You are sending %s %s in order to receive %s ksh",
			qouteInputAmount, swapData.ActiveSwapFromSym, inputStr,
		)

		return res, nil
	}

	// use the stored max RAT
	maxRATValue, err := strconv.ParseFloat(swapData.ActiveSwapMaxAmount, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the swapMaxAmount to a float", "error", err)
		return res, err
	}

	if inputAmount > maxRATValue {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = inputStr
		return res, nil
	}

	formattedAmount := fmt.Sprintf("%f", inputAmount)

	finalAmountStr, err := store.ParseAndScaleAmount(formattedAmount, swapData.ActiveSwapToDecimal)
	if err != nil {
		return res, err
	}

	// call the credit send API to get the reverse quote
	r, err := h.accountService.GetCreditSendReverseQuote(ctx, swapData.ActivePoolAddress, swapData.ActiveSwapFromAddress, swapData.ActiveSwapToAddress, finalAmountStr)
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
	quoteInputStr := store.ScaleDownBalance(sendInputAmount, swapData.ActiveSwapFromDecimal)
	// Format the quoteInputStr amount to 2 decimal places
	qouteInputAmount, _ := store.TruncateDecimalString(quoteInputStr, 2)

	res.Content = l.Get(
		"You are sending %s %s in order to receive %s ksh",
		qouteInputAmount, swapData.ActiveSwapFromSym, inputStr,
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

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	transactionType, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE)
	if err != nil {
		return res, err
	}

	if string(transactionType) == "normal" {
		// Call TokenTransfer for the normal transaction
		data, err := store.ReadTransactionData(ctx, h.userdataStore, sessionId)
		if err != nil {
			return res, err
		}

		finalAmountStr, err := store.ParseAndScaleAmount(data.Amount, data.ActiveDecimal)
		if err != nil {
			return res, err
		}

		tokenTransfer, err := h.accountService.TokenTransfer(ctx, finalAmountStr, data.PublicKey, mpesaAddress, data.ActiveAddress)
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_call_error)
			res.Content = l.Get("Your request failed. Please try again later.")
			logg.ErrorCtxf(ctx, "failed on TokenTransfer", "error", err)
			return res, nil
		}

		logg.InfoCtxf(ctx, "TokenTransfer normal", "trackingId", tokenTransfer.TrackingId)

		res.Content = l.Get("Your request has been sent. You will receive %s ksh", data.TemporaryValue)

		res.FlagReset = append(res.FlagReset, flag_account_authorized)
		return res, nil

	}

	swapAmount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_AMOUNT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read swapAmount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "error", err)
		return res, err
	}

	swapAmountStr := string(swapAmount)

	// Call the poolSwap API
	poolSwap, err := h.accountService.PoolSwap(ctx, swapAmountStr, swapData.PublicKey, swapData.ActiveSwapFromAddress, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	logg.InfoCtxf(ctx, "poolSwap", "swapTrackingId", poolSwap.TrackingId)

	finalKshStr, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		return res, err
	}

	amount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)
	if err != nil {
		return res, err
	}

	// Initiate a send to mpesa after the swap
	tokenTransfer, err := h.accountService.TokenTransfer(ctx, string(amount), swapData.PublicKey, mpesaAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on TokenTransfer after swap", "error", err)
		return res, nil
	}

	logg.InfoCtxf(ctx, "final TokenTransfer after swap", "trackingId", tokenTransfer.TrackingId)

	res.Content = l.Get("Your request has been sent. You will receive %s ksh", finalKshStr)
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
		"Enter the amount of Mpesa to send: (Minimum %s Ksh)\n",
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

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore
	sendRate := config.MpesaSendRate()

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

	estimateValue := kshAmount / sendRate
	estimateStr := fmt.Sprintf("%f", estimateValue)
	estimateFormatted, _ := store.TruncateDecimalString(estimateStr, 0)

	res.Content = l.Get(
		"You will get a prompt for your M-Pesa PIN shortly to send %s ksh and receive %s cUSD",
		inputStr, estimateFormatted,
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

	// Call the trigger onramp API
	triggerOnramp, err := h.accountService.MpesaTriggerOnramp(ctx, string(publicKey), phoneNumber, defaultAsset, string(amount))
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
