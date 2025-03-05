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
		"ActiveSwapFromSym":     storedb.DATA_ACTIVE_SWAP_FROM_SYM,
		"ActiveSwapFromDecimal": storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL,
		"ActiveSwapFromAddress": storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS,
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
		"PublicKey":             storedb.DATA_PUBLIC_KEY,
		"ActiveSwapMaxAmount":   storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT,
		"ActiveSwapFromDecimal": storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL,
		"ActivePoolAddress":     storedb.DATA_ACTIVE_POOL_ADDRESS,
		"ActiveSwapFromAddress": storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS,
		"ActiveSwapFromSym":     storedb.DATA_ACTIVE_SWAP_FROM_SYM,
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
func GetSwapFromVoucherData(ctx context.Context, db storedb.PrefixDb, input string) (*dataserviceapi.TokenHoldings, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_POOL_FROM_SYMBOLS,
		storedb.DATA_POOL_FROM_BALANCES,
		storedb.DATA_POOL_FROM_DECIMALS,
		storedb.DATA_POOL_FROM_ADDRESSES,
	}
	data := make(map[storedb.DataTyp]string)

	for _, key := range keys {
		value, err := db.Get(ctx, storedb.ToBytes(key))
		if err != nil {
			return nil, fmt.Errorf("failed to get prefix key %x: %v", storedb.ToBytes(key), err)
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
		TokenSymbol:     string(symbol),
		Balance:         string(balance),
		TokenDecimals:   string(decimal),
		ContractAddress: string(address),
	}, nil
}

// UpdateSwapFromVoucherData updates the active swap to voucher data in the DataStore.
func UpdateSwapFromVoucherData(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.TokenHoldings) error {
	logg.TraceCtxf(ctx, "dtal", "data", data)
	// Active swap from voucher data entries
	activeEntries := map[storedb.DataTyp][]byte{
		storedb.DATA_ACTIVE_SWAP_FROM_SYM:     []byte(data.TokenSymbol),
		storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL: []byte(data.TokenDecimals),
		storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS: []byte(data.ContractAddress),
	}

	// Write active data
	for key, value := range activeEntries {
		if err := store.WriteEntry(ctx, sessionId, key, value); err != nil {
			return err
		}
	}

	return nil
}

// GetSwapToVoucherData retrieves and matches voucher data
func GetSwapToVoucherData(ctx context.Context, db storedb.PrefixDb, input string) (*dataserviceapi.TokenHoldings, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_POOL_TO_SYMBOLS,
		storedb.DATA_POOL_TO_BALANCES,
		storedb.DATA_POOL_TO_DECIMALS,
		storedb.DATA_POOL_TO_ADDRESSES,
	}
	data := make(map[storedb.DataTyp]string)

	for _, key := range keys {
		value, err := db.Get(ctx, storedb.ToBytes(key))
		if err != nil {
			return nil, fmt.Errorf("failed to get prefix key %x: %v", storedb.ToBytes(key), err)
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
		TokenSymbol:     string(symbol),
		Balance:         string(balance),
		TokenDecimals:   string(decimal),
		ContractAddress: string(address),
	}, nil
}

// UpdateSwapToVoucherData updates the active swap to voucher data in the DataStore.
func UpdateSwapToVoucherData(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.TokenHoldings) error {
	logg.TraceCtxf(ctx, "dtal", "data", data)
	// Active swap to voucher data entries
	activeEntries := map[storedb.DataTyp][]byte{
		storedb.DATA_ACTIVE_SWAP_TO_SYM:     []byte(data.TokenSymbol),
		storedb.DATA_ACTIVE_SWAP_TO_DECIMAL: []byte(data.TokenDecimals),
		storedb.DATA_ACTIVE_SWAP_TO_ADDRESS: []byte(data.ContractAddress),
	}

	// Write active data
	for key, value := range activeEntries {
		if err := store.WriteEntry(ctx, sessionId, key, value); err != nil {
			return err
		}
	}

	return nil
}
