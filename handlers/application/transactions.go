package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/go-vise/resource"
)

// CheckTransactions retrieves the transactions from the API using the "PublicKey" and stores to prefixDb.
func (h *MenuHandlers) CheckTransactions(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_no_transfers, _ := h.flagManager.GetFlag("flag_no_transfers")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_error")

	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Fetch transactions from the API using the public key
	transactionsResp, err := h.accountService.FetchTransactions(ctx, string(publicKey))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		logg.ErrorCtxf(ctx, "failed on FetchTransactions", "error", err)
		return res, err
	}
	res.FlagReset = append(res.FlagReset, flag_api_error)

	// Return if there are no transactions
	if len(transactionsResp) == 0 {
		res.FlagSet = append(res.FlagSet, flag_no_transfers)
		return res, nil
	}

	data := store.ProcessTransfers(transactionsResp)

	// Store all transaction data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_TX_SENDERS:    data.Senders,
		storedb.DATA_TX_RECIPIENTS: data.Recipients,
		storedb.DATA_TX_VALUES:     data.TransferValues,
		storedb.DATA_TX_ADDRESSES:  data.Addresses,
		storedb.DATA_TX_HASHES:     data.TxHashes,
		storedb.DATA_TX_DATES:      data.Dates,
		storedb.DATA_TX_SYMBOLS:    data.Symbols,
		storedb.DATA_TX_DECIMALS:   data.Decimals,
	}

	for key, value := range dataMap {
		if err := h.prefixDb.Put(ctx, []byte(storedb.ToBytes(key)), []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "failed to write to prefixDb", "error", err)
			return res, err
		}
	}

	res.FlagReset = append(res.FlagReset, flag_no_transfers)

	return res, nil
}

// GetTransactionsList fetches the list of transactions and formats them.
func (h *MenuHandlers) GetTransactionsList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Read transactions from the store and format them
	TransactionSenders, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_SENDERS))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionSenders from prefixDb", "error", err)
		return res, err
	}
	TransactionSyms, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_SYMBOLS))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionSyms from prefixDb", "error", err)
		return res, err
	}
	TransactionValues, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_VALUES))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionValues from prefixDb", "error", err)
		return res, err
	}
	TransactionDates, err := h.prefixDb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_DATES))
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read the TransactionDates from prefixDb", "error", err)
		return res, err
	}

	// Parse the data
	senders := strings.Split(string(TransactionSenders), "\n")
	syms := strings.Split(string(TransactionSyms), "\n")
	values := strings.Split(string(TransactionValues), "\n")
	dates := strings.Split(string(TransactionDates), "\n")

	var formattedTransactions []string
	for i := 0; i < len(senders); i++ {
		sender := strings.TrimSpace(senders[i])
		sym := strings.TrimSpace(syms[i])
		value := strings.TrimSpace(values[i])
		date := strings.Split(strings.TrimSpace(dates[i]), " ")[0]

		status := "Received"
		if sender == string(publicKey) {
			status = "Sent"
		}

		// Use the ReplaceSeparator function for the menu separator
		transactionLine := fmt.Sprintf("%d%s%s %s %s %s", i+1, h.ReplaceSeparatorFunc(":"), status, value, sym, date)
		formattedTransactions = append(formattedTransactions, transactionLine)
	}

	res.Content = strings.Join(formattedTransactions, "\n")

	return res, nil
}

// ViewTransactionStatement retrieves the transaction statement
// and displays it to the user.
func (h *MenuHandlers) ViewTransactionStatement(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	userStore := h.userdataStore
	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	flag_incorrect_statement, _ := h.flagManager.GetFlag("flag_incorrect_statement")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "11" || inputStr == "22" {
		res.FlagReset = append(res.FlagReset, flag_incorrect_statement)
		return res, nil
	}

	// Convert input string to integer
	index, err := strconv.Atoi(strings.TrimSpace(inputStr))
	if err != nil {
		return res, fmt.Errorf("invalid input: must be a number between 1 and 10")
	}

	if index < 1 || index > 10 {
		return res, fmt.Errorf("invalid input: index must be between 1 and 10")
	}

	statement, err := store.GetTransferData(ctx, h.prefixDb, string(publicKey), index)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve transfer data: %v", err)
	}

	if statement == "" {
		res.FlagSet = append(res.FlagSet, flag_incorrect_statement)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_statement)
	res.Content = statement

	return res, nil
}
