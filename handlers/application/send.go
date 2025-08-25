package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/common/identity"
	"git.grassecon.net/grassrootseconomics/common/phone"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/ethutils"
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

	senderSym, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read sender activeSym", "error", err)
		return *res, err
	}

	recipientActiveToken, _ := store.ReadEntry(ctx, formattedNumber, storedb.DATA_ACTIVE_SYM)

	txType := "swap"

	// recipient has no active token → normal transaction
	if recipientActiveToken == nil {
		txType = "normal"
	} else if senderSym != nil && string(senderSym) == string(recipientActiveToken) {
		// recipient has active token same as sender → normal transaction
		txType = "normal"
	}

	// save transaction type
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(txType)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write tx type", "type", txType, "error", err)
		return *res, err
	}

	// only save recipient’s active token if it exists
	if recipientActiveToken != nil {
		if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT_ACTIVE_TOKEN, recipientActiveToken); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write recipient active token", "error", err)
			return *res, err
		}
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
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte("normal")); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write tx type for address", "error", err)
		return *res, err
	}

	return *res, nil
}

func (h *MenuHandlers) handleAlias(ctx context.Context, sessionId, recipient string, res *resource.Result) (resource.Result, error) {
	store := h.userdataStore
	flag_invalid_recipient, _ := h.flagManager.GetFlag("flag_invalid_recipient")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	var AliasAddressResult string

	if strings.Contains(recipient, ".") {
		alias, err := h.accountService.CheckAliasAddress(ctx, recipient)
		if err == nil {
			AliasAddressResult = alias.Address
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
				AliasAddressResult = alias.Address
				break
			} else {
				res.FlagSet = append(res.FlagSet, flag_api_error)
				logg.ErrorCtxf(ctx, "Alias resolution failed", "alias", fqdn, "error", err)
				return *res, nil
			}
		}
	}

	if AliasAddressResult == "" {
		res.FlagSet = append(res.FlagSet, flag_invalid_recipient)
		res.Content = recipient
		return *res, nil
	}

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(AliasAddressResult)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to store alias recipient", "error", err)
		return *res, err
	}
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte("normal")); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write tx type for alias", "error", err)
		return *res, err
	}

	return *res, nil
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

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT_ACTIVE_TOKEN, []byte(""))
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
		res.Content = fmt.Sprintf("%s %s", formattedBalance, string(activeSym))
		return res, nil
	}

	// Get recipient token address
	recipientTokenAddress, err := h.getRecipientTokenAddress(ctx, sessionId)
	if err != nil {
		// fallback to normal
		res.Content = fmt.Sprintf("%s %s", formattedBalance, string(activeSym))
		return res, nil
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
	maxStr, err := h.calculateSwapMaxAmount(ctx, activePoolAddress, activeAddress, recipientTokenAddress, publicKey, activeDecimal)
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
	return
}

func (h *MenuHandlers) getRecipientTokenAddress(ctx context.Context, sessionId string) ([]byte, error) {
	store := h.userdataStore
	recipientPhone, err := store.ReadEntry(ctx, sessionId, storedb.DATA_RECIPIENT_PHONE_NUMBER)
	if err != nil {
		return nil, err
	}
	return store.ReadEntry(ctx, string(recipientPhone), storedb.DATA_ACTIVE_ADDRESS)
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
		flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")
		res.FlagSet = append(res.FlagSet, flag_api_error)
		res.Content = l.Get("Your request failed. Please try again later.")
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
