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
	recipientActiveToken, err := store.ReadEntry(ctx, formattedNumber, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read recipient activeSym", "error", err)
		return *res, err
	}

	txType := "swap"
	if senderSym != nil && recipientActiveToken != nil && string(senderSym) == string(recipientActiveToken) {
		txType = "normal"
	}
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_SEND_TRANSACTION_TYPE, []byte(txType)); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write tx type", "type", txType, "error", err)
		return *res, err
	}
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT_ACTIVE_TOKEN, recipientActiveToken); err != nil {
		logg.ErrorCtxf(ctx, "Failed to write recipient active token", "error", err)
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

// MaxAmount gets the current sender's balance from the store and sets it as
// the result content.
func (h *MenuHandlers) MaxAmount(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	store := h.userdataStore

	activeBal, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeBal entry with", "key", storedb.DATA_ACTIVE_BAL, "error", err)
		return res, err
	}

	res.Content = string(activeBal)

	return res, nil
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
