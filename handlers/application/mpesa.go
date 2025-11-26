package application

import (
	"context"
	"fmt"
	"strconv"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/common/hex"
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
	if inputStr == "9" {
		return res, nil
	}

	userStore := h.userdataStore

	// Fetch session data
	_, _, _, activeAddress, publicKey, activeDecimal, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, err
	}

	mpesaAddress := config.DefaultMpesaAddress()

	// Normalize the alias address to fetch mpesa's phone number
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

	// retrieve the max credit send amounts (I have KILIFI SAT, I want USD RAT)
	_, maxRAT, err := h.calculateSendCreditLimits(ctx, activePoolAddress, activeAddress, recipientActiveAddress, publicKey, activeDecimal, recipientActiveDecimal)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on calculateSendCreditLimits", "error", err)
		return res, nil
	}

	// Format to 2 decimal places
	formattedAmount, _ := store.TruncateDecimalString(maxRAT, 2)
	// Fallback if below minimum
	maxFloat, _ := strconv.ParseFloat(maxRAT, 64)
	if maxFloat < 0.1 {
		// return with low amount flag
		res.Content = formattedAmount
		res.FlagSet = append(res.FlagSet, flag_low_swap_amount)
		return res, nil
	}

	// Save max RAT amount to be used in validating the user's input
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, []byte(maxRAT))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap max amount (maxRAT)", "value", maxRAT, "error", err)
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

	rate := 129.5
	amountFloat, _ := strconv.ParseFloat(maxRAT, 64)
	amountKsh := amountFloat * rate

	kshStr := fmt.Sprintf("%f", amountKsh)

	// truncate to 0 decimal places
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

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	userStore := h.userdataStore

	swapData, err := store.ReadSwapPreviewData(ctx, userStore, sessionId)
	if err != nil {
		return res, err
	}

	// use the stored max RAT
	maxRATValue, err := strconv.ParseFloat(swapData.ActiveSwapMaxAmount, 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the swapMaxAmount to a float", "error", err)
		return res, err
	}

	// Input in Ksh
	kshAmount, err := strconv.ParseFloat(inputStr, 64)

	// divide by the rate
	rate := 129.5
	inputAmount := kshAmount / rate

	if err != nil || inputAmount > maxRATValue {
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
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed GetCreditSendReverseQuote poolSwap", "error", err)
		return res, nil
	}

	sendInputAmount := r.InputAmount   // amount of SAT that should be swapped (current KILIFI)
	sendOutputAmount := r.OutputAmount // amount of RAT that will be received (intended USDT)

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

	// store the user's input amount in the temporary value
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(inputStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write temporary inputStr entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", inputStr, "error", err)
		return res, err
	}

	res.Content = l.Get(
		"You are sending %s %s in order to receive %s ksh",
		qouteInputAmount, swapData.ActiveSwapFromSym, inputStr,
	)

	return res, nil
}

// InitiateGetMpesa calls the poolSwap and returns a confirmation based on the result.
func (h *MenuHandlers) InitiateGetMpesa(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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
	poolSwap, err := h.accountService.PoolSwap(ctx, swapAmountStr, swapData.PublicKey, swapData.ActiveSwapFromAddress, swapData.ActivePoolAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	swapTrackingId := poolSwap.TrackingId
	logg.InfoCtxf(ctx, "poolSwap", "swapTrackingId", swapTrackingId)

	// Initiate a send to mpesa
	mpesaAddress := config.DefaultMpesaAddress()

	finalKshStr, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		// invalid state
		return res, err
	}

	// read the amount that should be sent
	amount, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)
	if err != nil {
		// invalid state
		return res, err
	}

	// Call TokenTransfer with the expected swap amount
	tokenTransfer, err := h.accountService.TokenTransfer(ctx, string(amount), swapData.PublicKey, mpesaAddress, swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on TokenTransfer", "error", err)
		return res, nil
	}

	trackingId := tokenTransfer.TrackingId
	logg.InfoCtxf(ctx, "TokenTransfer", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. You will receive %s ksh.",
		finalKshStr,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}
