package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/common/hex"
	"git.grassecon.net/grassrootseconomics/common/identity"
	"git.grassecon.net/grassrootseconomics/common/phone"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote/http"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/ethutils"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// ValidateRecipient validates that the given input is valid.
func (h *MenuHandlers) ValidateRecipient(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore
	flag_invalid_recipient, _ := h.flagManager.GetFlag("flag_invalid_recipient")

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// remove white spaces
	recipient := strings.ReplaceAll(string(input), " ", "")
	if recipient == "0" {
		return res, nil
	}

	recipientType, err := identity.CheckRecipient(recipient)
	if err != nil {
		// Invalid recipient format (not a phone number, address, or valid alias format)
		res.FlagSet = append(res.FlagSet, flag_invalid_recipient)
		res.Content = recipient

		return res, nil
	}

	// save the recipient as the temporaryRecipient
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(recipient))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write temporaryRecipient entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", recipient, "error", err)
		return res, err
	}

	switch recipientType {
	case "phone number":
		return h.handlePhoneNumber(ctx, sessionId, recipient, &res)
	case "address":
		return h.handleAddress(ctx, sessionId, recipient, &res)
	case "alias":
		return h.handleAlias(ctx, sessionId, recipient, &res)
	}

	return res, nil
}

func (h *MenuHandlers) handlePhoneNumber(ctx context.Context, sessionId, recipient string, res *resource.Result) (resource.Result, error) {
	store := h.userdataStore
	flag_invalid_recipient_with_invite, _ := h.flagManager.GetFlag("flag_invalid_recipient_with_invite")

	formattedNumber, err := phone.FormatPhoneNumber(recipient)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to format phone number", "recipient", recipient, "error", err)
		return *res, err
	}

	publicKey, err := store.ReadEntry(ctx, formattedNumber, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		if db.IsNotFound(err) {
			logg.InfoCtxf(ctx, "Unregistered phone number", "recipient", recipient)
			res.FlagSet = append(res.FlagSet, flag_invalid_recipient_with_invite)
			res.Content = recipient
			return *res, nil
		}
		logg.ErrorCtxf(ctx, "Failed to read publicKey", "error", err)
		return *res, err
	}

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, publicKey); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write recipient", "value", string(publicKey), "error", err)
		return *res, err
	}

	// Delegate to shared logic
	if err := h.determineAndSaveTransactionType(ctx, sessionId, publicKey, []byte(formattedNumber)); err != nil {
		return *res, err
	}

	return *res, nil
}

func (h *MenuHandlers) handleAddress(ctx context.Context, sessionId, recipient string, res *resource.Result) (resource.Result, error) {
	store := h.userdataStore
	address := ethutils.ChecksumAddress(recipient)

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(address)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write recipient address", "error", err)
		return *res, err
	}

	// normalize the address to fetch the recipient's phone number
	publicKeyNormalized, err := hex.NormalizeHex(address)
	if err != nil {
		return *res, err
	}

	// get the recipient's phone number from the address
	recipientPhoneNumber, err := store.ReadEntry(ctx, publicKeyNormalized, storedb.DATA_PUBLIC_KEY_REVERSE)
	if err != nil || len(recipientPhoneNumber) == 0 {
		logg.WarnCtxf(ctx, "Recipient address not registered, switching to normal transaction", "address", address)
		recipientPhoneNumber = nil
	}

	if err := h.determineAndSaveTransactionType(ctx, sessionId, []byte(address), recipientPhoneNumber); err != nil {
		return *res, err
	}

	return *res, nil
}

func (h *MenuHandlers) handleAlias(ctx context.Context, sessionId, recipient string, res *resource.Result) (resource.Result, error) {
	store := h.userdataStore
	flag_invalid_recipient, _ := h.flagManager.GetFlag("flag_invalid_recipient")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	var aliasAddressResult string

	if strings.Contains(recipient, ".") {
		alias, err := h.accountService.CheckAliasAddress(ctx, recipient)
		if err == nil {
			aliasAddressResult = alias.Address
		} else {
			logg.ErrorCtxf(ctx, "Failed to resolve alias", "alias", recipient, "error", err)
		}
	} else {
		for _, domain := range config.SearchDomains() {
			fqdn := fmt.Sprintf("%s.%s", recipient, domain)
			logg.InfoCtxf(ctx, "Trying alias", "fqdn", fqdn)

			alias, err := h.accountService.CheckAliasAddress(ctx, fqdn)
			if err == nil {
				res.FlagReset = append(res.FlagReset, flag_api_error)
				aliasAddressResult = alias.Address
				break
			} else {
				res.FlagSet = append(res.FlagSet, flag_api_error)
				logg.ErrorCtxf(ctx, "Alias resolution failed", "alias", fqdn, "error", err)
				return *res, nil
			}
		}
	}

	if aliasAddressResult == "" {
		res.FlagSet = append(res.FlagSet, flag_invalid_recipient)
		res.Content = recipient
		return *res, nil
	}

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(aliasAddressResult)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to store alias recipient", "error", err)
		return *res, err
	}

	// Normalize the alias address to fetch the recipient's phone number
	publicKeyNormalized, err := hex.NormalizeHex(aliasAddressResult)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to normalize alias address", "address", aliasAddressResult, "error", err)
		return *res, err
	}

	// get the recipient's phone number from the address
	recipientPhoneNumber, err := store.ReadEntry(ctx, publicKeyNormalized, storedb.DATA_PUBLIC_KEY_REVERSE)
	if err != nil || len(recipientPhoneNumber) == 0 {
		logg.WarnCtxf(ctx, "Alias address not registered, switching to normal transaction", "address", aliasAddressResult)
		recipientPhoneNumber = nil
	}

	if err := h.determineAndSaveTransactionType(ctx, sessionId, []byte(aliasAddressResult), recipientPhoneNumber); err != nil {
		return *res, err
	}

	return *res, nil
}

// determineAndSaveTransactionType centralizes transaction-type logic and recipient info persistence.
// It expects the session to already have the recipient's public key (address) written.
func (h *MenuHandlers) determineAndSaveTransactionType(
	ctx context.Context,
	sessionId string,
	publicKey []byte,
	recipientPhoneNumber []byte,
) error {
	store := h.userdataStore
	txType := "swap"

	// Read sender's active address
	senderActiveAddress, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read sender active address", "error", err)
		return err
	}

	var recipientActiveAddress []byte
	if recipientPhoneNumber != nil {
		recipientActiveAddress, _ = store.ReadEntry(ctx, string(recipientPhoneNumber), storedb.DATA_ACTIVE_ADDRESS)
	}

	// recipient has no active token → normal transaction
	if recipientActiveAddress == nil {
		txType = "normal"
	} else if senderActiveAddress != nil && string(senderActiveAddress) == string(recipientActiveAddress) {
		// recipient has active token same as sender → normal transaction
		txType = "normal"
	}

	// Save the transaction type
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(txType)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write transaction type", "type", txType, "error", err)
		return err
	}

	// Save the recipient's phone number only if it exists
	if recipientPhoneNumber != nil {
		if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER, recipientPhoneNumber); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write recipient phone number", "type", txType, "error", err)
			return err
		}
	} else {
		logg.InfoCtxf(ctx, "No recipient phone number found for public key", "publicKey", string(publicKey))
	}

	return nil
}

// TransactionReset resets the previous transaction data (Recipient and Amount)
// as well as the invalid flags.
func (h *MenuHandlers) TransactionReset(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_invalid_recipient, _ := h.flagManager.GetFlag("flag_invalid_recipient")
	flag_invalid_recipient_with_invite, _ := h.flagManager.GetFlag("flag_invalid_recipient_with_invite")
	store := h.userdataStore
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(""))
	if err != nil {
		return res, nil
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(""))
	if err != nil {
		return res, nil
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(""))
	if err != nil {
		return res, nil
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER, []byte(""))
	if err != nil {
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_invalid_recipient, flag_invalid_recipient_with_invite)

	return res, nil
}

// ResetTransactionAmount resets the transaction amount and invalid flag.
func (h *MenuHandlers) ResetTransactionAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	store := h.userdataStore
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(""))
	if err != nil {
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_invalid_amount)

	return res, nil
}

// MaxAmount checks the transaction type to determine the displayed max amount.
// If the transaction type is "swap", it checks the max swappable amount and sets this as the content.
// If the transaction type is "normal", gets the current sender's balance from the store and sets it as
// the result content.
func (h *MenuHandlers) MaxAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")
	flag_swap_transaction, _ := h.flagManager.GetFlag("flag_swap_transaction")
	userStore := h.userdataStore

	// Fetch session data
	transactionType, activeBal, activeSym, activeAddress, publicKey, activeDecimal, err := h.getSessionData(ctx, sessionId)
	if err != nil {
		return res, err
	}

	// Format the active balance amount to 2 decimal places
	formattedBalance, _ := store.TruncateDecimalString(string(activeBal), 2)

	// If normal transaction, return balance
	if string(transactionType) == "normal" {
		res.FlagReset = append(res.FlagReset, flag_swap_transaction)
		res.Content = fmt.Sprintf("%s %s", formattedBalance, string(activeSym))
		return res, nil
	}

	res.FlagSet = append(res.FlagSet, flag_swap_transaction)

	// Get the recipient's phone number to read other data items
	recipientPhoneNumber, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER)
	if err != nil {
		// invalid state
		return res, err
	}
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
	if err != nil || !canSwap.CanSwapFrom {
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_error)
			logg.ErrorCtxf(ctx, "failed on CheckTokenInPool", "error", err)
		}
		res.Content = fmt.Sprintf("%s %s", formattedBalance, string(activeSym))
		return res, err
	}

	// Calculate max swappable amount
	maxStr, err := h.calculateSwapMaxAmount(ctx, activePoolAddress, activeAddress, recipientActiveAddress, publicKey, activeDecimal)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		return res, err
	}

	// Fallback if below minimum
	maxFloat, _ := strconv.ParseFloat(maxStr, 64)
	if maxFloat < 0.1 {
		res.Content = fmt.Sprintf("%s %s", formattedBalance, string(activeSym))
		return res, nil
	}

	// Save max swap amount and return
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT, []byte(maxStr))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write swap max amount", "value", maxStr, "error", err)
		return res, err
	}

	// save swap related data for the swap preview
	metadata := &dataserviceapi.TokenHoldings{
		TokenSymbol:   string(recipientActiveSym),
		Balance:       formattedBalance, //not used
		TokenDecimals: string(recipientActiveDecimal),
		TokenAddress:  string(recipientActiveAddress),
	}

	// Store the active swap_to data
	if err := store.UpdateSwapToVoucherData(ctx, userStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateSwapToVoucherData", "error", err)
		return res, err
	}

	res.Content = fmt.Sprintf("%s %s", maxStr, string(activeSym))
	return res, nil
}

func (h *MenuHandlers) getSessionData(ctx context.Context, sessionId string) (transactionType, activeBal, activeSym, activeAddress, publicKey, activeDecimal []byte, err error) {
	store := h.userdataStore

	transactionType, err = store.ReadEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE)
	if err != nil {
		return
	}
	activeBal, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		return
	}
	activeAddress, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		return
	}
	activeSym, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		return
	}
	publicKey, err = store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		return
	}
	activeDecimal, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_DECIMAL)
	if err != nil {
		return
	}
	return
}

func (h *MenuHandlers) getRecipientData(ctx context.Context, sessionId string) (recipientActiveSym, recipientActiveAddress, recipientActiveDecimal []byte, err error) {
	store := h.userdataStore

	recipientActiveSym, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		return
	}
	recipientActiveAddress, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		return
	}
	recipientActiveDecimal, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_DECIMAL)
	if err != nil {
		return
	}
	return
}

func (h *MenuHandlers) resolveActivePoolAddress(ctx context.Context, sessionId string) ([]byte, error) {
	store := h.userdataStore
	addr, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_ADDRESS)
	if err == nil {
		return addr, nil
	}
	if db.IsNotFound(err) {
		defaultAddr := []byte(config.DefaultPoolAddress())
		if err := store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_ADDRESS, defaultAddr); err != nil {
			logg.ErrorCtxf(ctx, "failed to write default pool address", "error", err)
			return nil, err
		}
		return defaultAddr, nil
	}
	logg.ErrorCtxf(ctx, "failed to read active pool address", "error", err)
	return nil, err
}

func (h *MenuHandlers) calculateSwapMaxAmount(ctx context.Context, poolAddress, fromAddress, toAddress, publicKey, decimal []byte) (string, error) {
	swapLimit, err := h.accountService.GetSwapFromTokenMaxLimit(
		ctx,
		string(poolAddress),
		string(fromAddress),
		string(toAddress),
		string(publicKey),
	)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetSwapFromTokenMaxLimit", "error", err)
		return "", err
	}

	scaled := store.ScaleDownBalance(swapLimit.Max, string(decimal))

	formattedAmount, _ := store.TruncateDecimalString(string(scaled), 2)
	return formattedAmount, nil
}

// ValidateAmount ensures that the given input is a valid amount and that
// it is not more than the current balance.
func (h *MenuHandlers) ValidateAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_invalid_amount, _ := h.flagManager.GetFlag("flag_invalid_amount")
	userStore := h.userdataStore

	var balanceValue float64

	// retrieve the active balance
	activeBal, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
		return res, err
	}
	balanceValue, err = strconv.ParseFloat(string(activeBal), 64)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to convert the activeBal to a float", "error", err)
		return res, err
	}

	// Extract numeric part from the input amount
	amountStr := strings.TrimSpace(string(input))
	inputAmount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = amountStr
		return res, nil
	}

	if inputAmount > balanceValue {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = amountStr
		return res, nil
	}

	// Format the amount to 2 decimal places before saving (truncated)
	formattedAmount, err := store.TruncateDecimalString(amountStr, 2)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_invalid_amount)
		res.Content = amountStr
		return res, nil
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(formattedAmount))
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to write amount entry with", "key", storedb.DATA_AMOUNT, "value", formattedAmount, "error", err)
		return res, err
	}

	res.Content = formattedAmount
	return res, nil
}

// GetRecipient returns the transaction recipient phone number from the gdbm.
func (h *MenuHandlers) GetRecipient(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore
	recipient, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if len(recipient) == 0 {
		logg.ErrorCtxf(ctx, "recipient is empty", "key", storedb.DATA_TEMPORARY_VALUE)
		return res, fmt.Errorf("data error encountered")
	}

	res.Content = string(recipient)

	return res, nil
}

// GetSender returns the sessionId (phoneNumber).
func (h *MenuHandlers) GetSender(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	res.Content = sessionId

	return res, nil
}

// GetAmount retrieves the amount from teh Gdbm Db.
func (h *MenuHandlers) GetAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore

	// retrieve the active symbol
	activeSym, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
		return res, err
	}

	amount, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_AMOUNT)

	res.Content = fmt.Sprintf("%s %s", string(amount), string(activeSym))

	return res, nil
}

// InitiateTransaction calls the TokenTransfer and returns a confirmation based on the result.
func (h *MenuHandlers) InitiateTransaction(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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

	data, err := store.ReadTransactionData(ctx, h.userdataStore, sessionId)
	if err != nil {
		return res, err
	}

	finalAmountStr, err := store.ParseAndScaleAmount(data.Amount, data.ActiveDecimal)
	if err != nil {
		return res, err
	}

	// Call TokenTransfer
	r, err := h.accountService.TokenTransfer(ctx, finalAmountStr, data.PublicKey, data.Recipient, data.ActiveAddress)
	if err != nil {
		var apiErr *http.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.Code {
			case "E10":
				res.Content = l.Get("Only USD vouchers are allowed to mpesa.sarafu.eth.")
			default:
				res.Content = l.Get("Your request failed. Please try again later.")
			}
		} else {
			res.Content = l.Get("An unexpected error occurred. Please try again later.")
		}

		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on TokenTransfer", "error", err)
		return res, nil
	}

	trackingId := r.TrackingId
	logg.InfoCtxf(ctx, "TokenTransfer", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. %s will receive %s %s from %s.",
		data.TemporaryValue,
		data.Amount,
		data.ActiveSym,
		sessionId,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized)
	return res, nil
}

// TransactionSwapPreview displays the send swap preview and estimates
func (h *MenuHandlers) TransactionSwapPreview(ctx context.Context, sym string, input []byte) (resource.Result, error) {
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

	recipientPhoneNumber, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER)
	if err != nil {
		// invalid state
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
		"%s will receive %s %s",
		string(recipientPhoneNumber), qouteStr, swapData.ActiveSwapToSym,
	)

	return res, nil
}

// TransactionInitiateSwap calls the poolSwap and returns a confirmation based on the result.
func (h *MenuHandlers) TransactionInitiateSwap(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_account_authorized, _ := h.flagManager.GetFlag("flag_account_authorized")
	flag_swap_transaction, _ := h.flagManager.GetFlag("flag_swap_transaction")

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
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on poolSwap", "error", err)
		return res, nil
	}

	swapTrackingId := poolSwap.TrackingId
	logg.InfoCtxf(ctx, "poolSwap", "swapTrackingId", swapTrackingId)

	// Initiate a send
	recipientPublicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_RECIPIENT)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read swapAmount entry with", "key", storedb.DATA_ACTIVE_SWAP_AMOUNT, "error", err)
		return res, err
	}
	recipientPhoneNumber, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER)
	if err != nil {
		// invalid state
		return res, err
	}

	// Call TokenTransfer
	tokenTransfer, err := h.accountService.TokenTransfer(ctx, swapAmountStr, swapData.PublicKey, string(recipientPublicKey), swapData.ActiveSwapToAddress)
	if err != nil {
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
		logg.ErrorCtxf(ctx, "failed on TokenTransfer", "error", err)
		return res, nil
	}

	trackingId := tokenTransfer.TrackingId
	logg.InfoCtxf(ctx, "TokenTransfer", "trackingId", trackingId)

	res.Content = l.Get(
		"Your request has been sent. %s will receive %s %s from %s.",
		string(recipientPhoneNumber),
		swapData.TemporaryValue,
		swapData.ActiveSwapToSym,
		sessionId,
	)

	res.FlagReset = append(res.FlagReset, flag_account_authorized, flag_swap_transaction)
	return res, nil
}

// ClearTransactionTypeFlag resets the flag when a user goes back.
func (h *MenuHandlers) ClearTransactionTypeFlag(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_swap_transaction, _ := h.flagManager.GetFlag("flag_swap_transaction")

	inputStr := string(input)
	if inputStr == "0" {
		res.FlagReset = append(res.FlagReset, flag_swap_transaction)
		return res, nil
	}

	return res, nil
}
