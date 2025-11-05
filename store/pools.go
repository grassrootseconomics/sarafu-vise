package store

import (
	"context"
	"fmt"
	"strings"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

// PoolsMetadata helps organize data fields
type PoolsMetadata struct {
	PoolNames             string
	PoolSymbols           string
	PoolContractAdrresses string
}

// ProcessPools converts pools into formatted strings
func ProcessPools(pools []dataserviceapi.PoolDetails) PoolsMetadata {
	var data PoolsMetadata
	var poolNames, poolSymbols, poolContractAdrresses []string

	for i, p := range pools {
		poolNames = append(poolNames, fmt.Sprintf("%d:%s", i+1, p.PoolName))
		poolSymbols = append(poolSymbols, fmt.Sprintf("%d:%s", i+1, p.PoolSymbol))
		poolContractAdrresses = append(poolContractAdrresses, fmt.Sprintf("%d:%s", i+1, p.PoolContractAdrress))
	}

	data.PoolNames = strings.Join(poolNames, "\n")
	data.PoolSymbols = strings.Join(poolSymbols, "\n")
	data.PoolContractAdrresses = strings.Join(poolContractAdrresses, "\n")

	return data
}

// GetPoolData retrieves and matches pool data
// if no match is found, it fetches the API with the symbol
func GetPoolData(ctx context.Context, store DataStore, sessionId string, input string) (*dataserviceapi.PoolDetails, error) {
	keys := []storedb.DataTyp{
		storedb.DATA_POOL_NAMES,
		storedb.DATA_POOL_SYMBOLS,
		storedb.DATA_POOL_ADDRESSES,
	}
	data := make(map[storedb.DataTyp]string)

	for _, key := range keys {
		value, err := store.ReadEntry(ctx, sessionId, key)
		if err != nil {
			return nil, fmt.Errorf("failed to get data key %x: %v", key, err)
		}
		data[key] = string(value)
	}

	name, symbol, address := MatchPool(input,
		data[storedb.DATA_POOL_NAMES],
		data[storedb.DATA_POOL_SYMBOLS],
		data[storedb.DATA_POOL_ADDRESSES],
	)

	if symbol == "" {
		return nil, nil
	}

	return &dataserviceapi.PoolDetails{
		PoolName:            string(name),
		PoolSymbol:          string(symbol),
		PoolContractAdrress: string(address),
	}, nil
}

// MatchPool finds the matching pool name, symbol and pool contract address based on the input.
func MatchPool(input, names, symbols, addresses string) (name, symbol, address string) {
	nameList := strings.Split(names, "\n")
	symList := strings.Split(symbols, "\n")
	addrList := strings.Split(addresses, "\n")

	for i, sym := range symList {
		parts := strings.SplitN(sym, ":", 2)

		if input == parts[0] || strings.EqualFold(input, parts[1]) {
			symbol = parts[1]
			if i < len(nameList) {
				name = strings.SplitN(nameList[i], ":", 2)[1]
			}
			if i < len(addrList) {
				address = strings.SplitN(addrList[i], ":", 2)[1]
			}
			break
		}
	}
	return
}

// StoreTemporaryPool saves pool metadata as temporary entries in the DataStore.
func StoreTemporaryPool(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.PoolDetails) error {
	tempData := fmt.Sprintf("%s,%s,%s", data.PoolName, data.PoolSymbol, data.PoolContractAdrress)

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(tempData)); err != nil {
		return err
	}

	return nil
}

// GetTemporaryPoolData retrieves temporary pool metadata from the DataStore.
func GetTemporaryPoolData(ctx context.Context, store DataStore, sessionId string) (*dataserviceapi.PoolDetails, error) {
	temp_data, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		return nil, err
	}

	values := strings.SplitN(string(temp_data), ",", 3)

	data := &dataserviceapi.PoolDetails{}

	data.PoolName = values[0]
	data.PoolSymbol = values[1]
	data.PoolContractAdrress = values[2]

	return data, nil
}

// UpdatePoolData updates the active pool data in the DataStore.
func UpdatePoolData(ctx context.Context, store DataStore, sessionId string, data *dataserviceapi.PoolDetails) error {
	logg.InfoCtxf(ctx, "UpdatePoolData", "data", data)
	// Active pool data entry
	activeEntries := map[storedb.DataTyp][]byte{
		storedb.DATA_ACTIVE_POOL_NAME:     []byte(data.PoolName),
		storedb.DATA_ACTIVE_POOL_SYM:     []byte(data.PoolSymbol),
		storedb.DATA_ACTIVE_POOL_ADDRESS: []byte(data.PoolContractAdrress),
	}

	// Write active data
	for key, value := range activeEntries {
		if err := store.WriteEntry(ctx, sessionId, key, value); err != nil {
			return err
		}
	}

	return nil
}