package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

// TransferMetadata helps organize data fields
type TransferMetadata struct {
	Senders        string
	Recipients     string
	TransferValues string
	Addresses      string
	TxHashes       string
	Dates          string
	Symbols        string
	Decimals       string
}

// ProcessTransfers converts transfers into formatted strings
func ProcessTransfers(transfers []dataserviceapi.Last10TxResponse) TransferMetadata {
	var data TransferMetadata
	var senders, recipients, transferValues, addresses, txHashes, dates, symbols, decimals []string

	for _, t := range transfers {
		senders = append(senders, t.Sender)
		recipients = append(recipients, t.Recipient)

		// Scale down the amount
		scaledBalance := ScaleDownBalance(t.TransferValue, t.TokenDecimals)
		transferValues = append(transferValues, scaledBalance)

		addresses = append(addresses, t.ContractAddress)
		txHashes = append(txHashes, t.TxHash)
		dates = append(dates, fmt.Sprintf("%s", t.DateBlock))
		symbols = append(symbols, t.TokenSymbol)
		decimals = append(decimals, t.TokenDecimals)
	}

	data.Senders = strings.Join(senders, "\n")
	data.Recipients = strings.Join(recipients, "\n")
	data.TransferValues = strings.Join(transferValues, "\n")
	data.Addresses = strings.Join(addresses, "\n")
	data.TxHashes = strings.Join(txHashes, "\n")
	data.Dates = strings.Join(dates, "\n")
	data.Symbols = strings.Join(symbols, "\n")
	data.Decimals = strings.Join(decimals, "\n")

	return data
}

// GetTransferData retrieves and matches transfer data
// returns a formatted string of the full transaction/statement
func GetTransferData(ctx context.Context, db storedb.PrefixDb, publicKey string, index int) (string, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_TX_SENDERS,
		storedb.DATA_TX_RECIPIENTS,
		storedb.DATA_TX_VALUES,
		storedb.DATA_TX_ADDRESSES,
		storedb.DATA_TX_HASHES,
		storedb.DATA_TX_DATES,
		storedb.DATA_TX_SYMBOLS,
	}
	data := make(map[storedb.DataTyp]string)

	for _, key := range keys {
		value, err := db.Get(ctx, storedb.ToBytes(key))
		if err != nil {
			return "", fmt.Errorf("failed to get %s: %v", storedb.ToBytes(key), err)
		}
		data[key] = string(value)
	}

	// Split the data
	senders := strings.Split(string(data[storedb.DATA_TX_SENDERS]), "\n")
	recipients := strings.Split(string(data[storedb.DATA_TX_RECIPIENTS]), "\n")
	values := strings.Split(string(data[storedb.DATA_TX_VALUES]), "\n")
	addresses := strings.Split(string(data[storedb.DATA_TX_ADDRESSES]), "\n")
	hashes := strings.Split(string(data[storedb.DATA_TX_HASHES]), "\n")
	dates := strings.Split(string(data[storedb.DATA_TX_DATES]), "\n")
	syms := strings.Split(string(data[storedb.DATA_TX_SYMBOLS]), "\n")

	// Check if index is within range
	if index < 1 || index > len(senders) {
		return "", fmt.Errorf("transaction not found: index %d out of range", index)
	}

	// Adjust for 0-based indexing
	i := index - 1
	transactionType := "Received"
	party := fmt.Sprintf("From: %s", strings.TrimSpace(senders[i]))
	if strings.TrimSpace(senders[i]) == publicKey {
		transactionType = "Sent"
		party = fmt.Sprintf("To: %s", strings.TrimSpace(recipients[i]))
	}

	formattedDate := formatDate(strings.TrimSpace(dates[i]))

	// Build the full transaction detail
	detail := fmt.Sprintf(
		"%s %s %s\n%s\nContract address: %s\nTxhash: %s\nDate: %s",
		transactionType,
		strings.TrimSpace(values[i]),
		strings.TrimSpace(syms[i]),
		party,
		strings.TrimSpace(addresses[i]),
		strings.TrimSpace(hashes[i]),
		formattedDate,
	)

	return detail, nil
}

// Helper function to format date in desired output
func formatDate(dateStr string) string {
	parsedDate, err := time.Parse("2006-01-02 15:04:05 -0700 MST", dateStr)
	if err != nil {
		fmt.Println("Error parsing date:", err)
		return ""
	}
	return parsedDate.Format("2006-01-02 03:04:05 PM")
}
