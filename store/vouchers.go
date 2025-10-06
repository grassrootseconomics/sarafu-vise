package store

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"git.defalsify.org/vise.git/logging"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

var (
	logg = logging.NewVanilla().WithDomain("vouchers").WithContextKey("SessionId")
)

// VoucherMetadata helps organize data fields
type VoucherMetadata struct {
	Symbols   string
	Balances  string
	Decimals  string
	Addresses string
}

// ProcessVouchers converts holdings into formatted strings
func ProcessVouchers(holdings []dataserviceapi.TokenHoldings) VoucherMetadata {
	var data VoucherMetadata
	var symbols, balances, decimals, addresses []string

	for i, h := range holdings {
		symbols = append(symbols, fmt.Sprintf("%d:%s", i+1, h.TokenSymbol))

		// Scale down the balance
		scaledBalance := ScaleDownBalance(h.Balance, h.TokenDecimals)

		balances = append(balances, fmt.Sprintf("%d:%s", i+1, scaledBalance))
		decimals = append(decimals, fmt.Sprintf("%d:%s", i+1, h.TokenDecimals))
		addresses = append(addresses, fmt.Sprintf("%d:%s", i+1, h.TokenAddress))
	}

	data.Symbols = strings.Join(symbols, "\n")
	data.Balances = strings.Join(balances, "\n")
	data.Decimals = strings.Join(decimals, "\n")
	data.Addresses = strings.Join(addresses, "\n")

	return data
}

func ScaleDownBalance(balance, decimals string) string {
	// Convert balance and decimals to big.Float
	bal := new(big.Float)
	bal.SetString(balance)

	dec, ok := new(big.Int).SetString(decimals, 10)
	if !ok {
		dec = big.NewInt(0) // Default to 0 decimals in case of conversion failure
	}

	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), dec, nil))
	scaledBalance := new(big.Float).Quo(bal, divisor)

	// Return the scaled balance without trailing decimals if it's an integer
	if scaledBalance.IsInt() {
		return scaledBalance.Text('f', 0)
	}
	return scaledBalance.Text('f', -1)
}

// GetVoucherData retrieves and matches voucher data
func GetVoucherData(ctx context.Context, store DataStore, sessionId string, input string) (*dataserviceapi.TokenHoldings, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_VOUCHER_SYMBOLS,
		storedb.DATA_VOUCHER_BALANCES,
		storedb.DATA_VOUCHER_DECIMALS,
		storedb.DATA_VOUCHER_ADDRESSES,
	}
	data := make(map[storedb.DataTyp]string)

	for _, key := range keys {
		value, err := store.ReadEntry(ctx, sessionId, key)
		if err != nil {
			return nil, fmt.Errorf("failed to get data key %x: %v", key, err)
		}
		data[key] = string(value)
	}

	symbol, balance, decimal, address := MatchVoucher(input,
		data[storedb.DATA_VOUCHER_SYMBOLS],
		data[storedb.DATA_VOUCHER_BALANCES],
		data[storedb.DATA_VOUCHER_DECIMALS],
		data[storedb.DATA_VOUCHER_ADDRESSES],
	)

	if symbol == "" {
		return nil, nil
	}

	return &dataserviceapi.TokenHoldings{
		TokenSymbol:   string(symbol),
		Balance:       string(balance),
		TokenDecimals: string(decimal),
		TokenAddress:  string(address),
	}, nil
}

// MatchVoucher finds the matching voucher symbol, balance, decimals and contract address based on the input.
func MatchVoucher(input, symbols, balances, decimals, addresses string) (symbol, balance, decimal, address string) {
	symList := strings.Split(symbols, "\n")
	balList := strings.Split(balances, "\n")
	decList := strings.Split(decimals, "\n")
	addrList := strings.Split(addresses, "\n")

	logg.Tracef("found", "symlist", symList, "syms", symbols, "input", input)
	for i, sym := range symList {
		parts := strings.SplitN(sym, ":", 2)

		if input == parts[0] || strings.EqualFold(input, parts[1]) {
			symbol = parts[1]
			if i < len(balList) {
				balance = strings.SplitN(balList[i], ":", 2)[1]
			}
			if i < len(decList) {
				decimal = strings.SplitN(decList[i], ":", 2)[1]
			}
			if i < len(addrList) {
				address = strings.SplitN(addrList[i], ":", 2)[1]
			}
			break
		}
	}
	return
}

// StoreTemporaryVoucher saves voucher metadata as temporary entries in the DataStore.
func StoreTemporaryVoucher(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.TokenHoldings) error {
	tempData := fmt.Sprintf("%s,%s,%s,%s", data.TokenSymbol, data.Balance, data.TokenDecimals, data.TokenAddress)

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(tempData)); err != nil {
		return err
	}

	return nil
}

// GetTemporaryVoucherData retrieves temporary voucher metadata from the DataStore.
func GetTemporaryVoucherData(ctx context.Context, store DataStore, sessionId string) (*dataserviceapi.TokenHoldings, error) {
	temp_data, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		return nil, err
	}

	values := strings.SplitN(string(temp_data), ",", 4)

	data := &dataserviceapi.TokenHoldings{}

	data.TokenSymbol = values[0]
	data.Balance = values[1]
	data.TokenDecimals = values[2]
	data.TokenAddress = values[3]

	return data, nil
}

// UpdateVoucherData updates the active voucher data in the DataStore.
func UpdateVoucherData(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.TokenHoldings) error {
	logg.TraceCtxf(ctx, "dtal", "data", data)
	// Active voucher data entries
	activeEntries := map[storedb.DataTyp][]byte{
		storedb.DATA_ACTIVE_SYM:     []byte(data.TokenSymbol),
		storedb.DATA_ACTIVE_BAL:     []byte(data.Balance),
		storedb.DATA_ACTIVE_DECIMAL: []byte(data.TokenDecimals),
		storedb.DATA_ACTIVE_ADDRESS: []byte(data.TokenAddress),
	}

	// Write active data
	for key, value := range activeEntries {
		if err := store.WriteEntry(ctx, sessionId, key, value); err != nil {
			return err
		}
	}

	return nil
}

// FormatVoucherList combines the voucher symbols with their balances (SRF 0.11)
func FormatVoucherList(ctx context.Context, symbolsData, balancesData string) []string {
	symbols := strings.Split(symbolsData, "\n")
	balances := strings.Split(balancesData, "\n")

	var combined []string
	for i := 0; i < len(symbols) && i < len(balances); i++ {
		symbolParts := strings.SplitN(symbols[i], ":", 2)
		balanceParts := strings.SplitN(balances[i], ":", 2)

		if len(symbolParts) == 2 && len(balanceParts) == 2 {
			index := strings.TrimSpace(symbolParts[0])
			symbol := strings.TrimSpace(symbolParts[1])
			rawBalance := strings.TrimSpace(balanceParts[1])

			formattedBalance, err := TruncateDecimalString(rawBalance, 2)
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to format balance", "balance", rawBalance, "error", err)
				formattedBalance = rawBalance
			}

			combined = append(combined, fmt.Sprintf("%s: %s %s", index, symbol, formattedBalance))
		}
	}
	return combined
}
