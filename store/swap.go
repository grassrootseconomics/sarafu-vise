package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

type SwapData struct {
	PublicKey             string
	ActivePoolAddress     string
	ActiveSwapFromSym     string
	ActiveSwapFromDecimal string
	ActiveSwapFromAddress string
	ActiveSwapToSym       string
	ActiveSwapToAddress   string
}

type SwapPreviewData struct {
	TemporaryValue        string
	PublicKey             string
	ActiveSwapMaxAmount   string
	ActiveSwapFromDecimal string
	ActivePoolAddress     string
	ActiveSwapFromAddress string
	ActiveSwapFromSym     string
	ActiveSwapToAddress   string
	ActiveSwapToSym       string
	ActiveSwapToDecimal   string
}

func ReadSwapData(ctx context.Context, store DataStore, sessionId string) (SwapData, error) {
	data := SwapData{}
	fieldToKey := map[string]storedb.DataTyp{
		"PublicKey":             storedb.DATA_PUBLIC_KEY,
		"ActivePoolAddress":     storedb.DATA_ACTIVE_POOL_ADDRESS,
		"ActiveSwapFromSym":     storedb.DATA_ACTIVE_SYM,
		"ActiveSwapFromDecimal": storedb.DATA_ACTIVE_DECIMAL,
		"ActiveSwapFromAddress": storedb.DATA_ACTIVE_ADDRESS,
		"ActiveSwapToSym":       storedb.DATA_ACTIVE_SWAP_TO_SYM,
		"ActiveSwapToAddress":   storedb.DATA_ACTIVE_SWAP_TO_ADDRESS,
	}

	v := reflect.ValueOf(&data).Elem()
	for fieldName, key := range fieldToKey {
		field := v.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			return data, errors.New("invalid struct field: " + fieldName)
		}

		value, err := ReadStringEntry(ctx, store, sessionId, key)
		if err != nil {
			return data, err
		}
		field.SetString(value)
	}

	return data, nil
}

func ReadSwapPreviewData(ctx context.Context, store DataStore, sessionId string) (SwapPreviewData, error) {
	data := SwapPreviewData{}
	fieldToKey := map[string]storedb.DataTyp{
		"TemporaryValue":        storedb.DATA_TEMPORARY_VALUE,
		"PublicKey":             storedb.DATA_PUBLIC_KEY,
		"ActiveSwapMaxAmount":   storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT,
		"ActiveSwapFromDecimal": storedb.DATA_ACTIVE_DECIMAL,
		"ActivePoolAddress":     storedb.DATA_ACTIVE_POOL_ADDRESS,
		"ActiveSwapFromAddress": storedb.DATA_ACTIVE_ADDRESS,
		"ActiveSwapFromSym":     storedb.DATA_ACTIVE_SYM,
		"ActiveSwapToAddress":   storedb.DATA_ACTIVE_SWAP_TO_ADDRESS,
		"ActiveSwapToSym":       storedb.DATA_ACTIVE_SWAP_TO_SYM,
		"ActiveSwapToDecimal":   storedb.DATA_ACTIVE_SWAP_TO_DECIMAL,
	}

	v := reflect.ValueOf(&data).Elem()
	for fieldName, key := range fieldToKey {
		field := v.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			return data, errors.New("invalid struct field: " + fieldName)
		}

		value, err := ReadStringEntry(ctx, store, sessionId, key)
		if err != nil {
			return data, err
		}
		field.SetString(value)
	}

	return data, nil
}

// GetSwapFromVoucherData retrieves and matches swap from voucher data
func GetSwapFromVoucherData(ctx context.Context, store DataStore, sessionId string, input string) (*dataserviceapi.TokenHoldings, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_POOL_FROM_SYMBOLS,
		storedb.DATA_POOL_FROM_BALANCES,
		storedb.DATA_POOL_FROM_DECIMALS,
		storedb.DATA_POOL_FROM_ADDRESSES,
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
		data[storedb.DATA_POOL_FROM_SYMBOLS],
		data[storedb.DATA_POOL_FROM_BALANCES],
		data[storedb.DATA_POOL_FROM_DECIMALS],
		data[storedb.DATA_POOL_FROM_ADDRESSES],
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

// GetSwapToVoucherData retrieves and matches token data
func GetSwapToVoucherData(ctx context.Context, store DataStore, sessionId string, input string) (*dataserviceapi.TokenHoldings, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_POOL_TO_SYMBOLS,
		storedb.DATA_POOL_TO_BALANCES,
		storedb.DATA_POOL_TO_DECIMALS,
		storedb.DATA_POOL_TO_ADDRESSES,
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
		data[storedb.DATA_POOL_TO_SYMBOLS],
		data[storedb.DATA_POOL_TO_BALANCES],
		data[storedb.DATA_POOL_TO_DECIMALS],
		data[storedb.DATA_POOL_TO_ADDRESSES],
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

// UpdateSwapToVoucherData updates the active swap to voucher data in the DataStore.
func UpdateSwapToVoucherData(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.TokenHoldings) error {
	logg.InfoCtxf(ctx, "UpdateSwapToVoucherData", "data", data)
	// Active swap to voucher data entries
	activeEntries := map[storedb.DataTyp][]byte{
		storedb.DATA_ACTIVE_SWAP_TO_ADDRESS: []byte(data.TokenAddress),
		storedb.DATA_ACTIVE_SWAP_TO_SYM:     []byte(data.TokenSymbol),
		storedb.DATA_ACTIVE_SWAP_TO_DECIMAL: []byte(data.TokenDecimals),
	}

	// Write active data
	for key, value := range activeEntries {
		if err := store.WriteEntry(ctx, sessionId, key, value); err != nil {
			return err
		}
	}

	return nil
}
