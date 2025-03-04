package store

import (
	"context"
	"fmt"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

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
		storedb.DATA_ACTIVE_SWAP_TO_SYM: []byte(data.TokenSymbol),
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
