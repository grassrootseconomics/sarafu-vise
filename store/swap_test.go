package store

import (
	"context"
	"testing"

	visedb "git.defalsify.org/vise.git/db"
	memdb "git.defalsify.org/vise.git/db/mem"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
	"github.com/stretchr/testify/require"
)

func TestReadSwapData(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"
	ctx, store := InitializeTestDb(t)

	// Test swap data
	swapData := map[storedb.DataTyp]string{
		storedb.DATA_PUBLIC_KEY:               publicKey,
		storedb.DATA_ACTIVE_POOL_ADDRESS:      "0x48a953cA5cf5298bc6f6Af3C608351f537AAcb9e",
		storedb.DATA_ACTIVE_SWAP_FROM_SYM:     "AMANI",
		storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL: "6",
		storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS: "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe",
		storedb.DATA_ACTIVE_SWAP_TO_SYM:       "cUSD",
		storedb.DATA_ACTIVE_SWAP_TO_ADDRESS:   "0x765DE816845861e75A25fCA122bb6898B8B1282a",
	}

	// Store the data
	for key, value := range swapData {
		if err := store.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	expectedResult := SwapData{
		PublicKey:             "0X13242618721",
		ActivePoolAddress:     "0x48a953cA5cf5298bc6f6Af3C608351f537AAcb9e",
		ActiveSwapFromSym:     "AMANI",
		ActiveSwapFromDecimal: "6",
		ActiveSwapFromAddress: "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe",
		ActiveSwapToSym:       "cUSD",
		ActiveSwapToAddress:   "0x765DE816845861e75A25fCA122bb6898B8B1282a",
	}

	data, err := ReadSwapData(ctx, store, sessionId)

	assert.NoError(t, err)
	assert.Equal(t, expectedResult, data)
}

func TestReadSwapPreviewData(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"
	ctx, store := InitializeTestDb(t)

	// Test swap preview data
	swapPreviewData := map[storedb.DataTyp]string{
		storedb.DATA_PUBLIC_KEY:               publicKey,
		storedb.DATA_ACTIVE_SWAP_MAX_AMOUNT:   "1339482",
		storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL: "6",
		storedb.DATA_ACTIVE_POOL_ADDRESS:      "0x48a953cA5cf5298bc6f6Af3C608351f537AAcb9e",
		storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS: "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe",
		storedb.DATA_ACTIVE_SWAP_FROM_SYM:     "AMANI",
		storedb.DATA_ACTIVE_SWAP_TO_ADDRESS:   "0x765DE816845861e75A25fCA122bb6898B8B1282a",
		storedb.DATA_ACTIVE_SWAP_TO_SYM:       "cUSD",
		storedb.DATA_ACTIVE_SWAP_TO_DECIMAL:   "18",
	}

	// Store the data
	for key, value := range swapPreviewData {
		if err := store.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	expectedResult := SwapPreviewData{
		PublicKey:             "0X13242618721",
		ActiveSwapMaxAmount:   "1339482",
		ActiveSwapFromDecimal: "6",
		ActivePoolAddress:     "0x48a953cA5cf5298bc6f6Af3C608351f537AAcb9e",
		ActiveSwapFromAddress: "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe",
		ActiveSwapFromSym:     "AMANI",
		ActiveSwapToAddress:   "0x765DE816845861e75A25fCA122bb6898B8B1282a",
		ActiveSwapToSym:       "cUSD",
		ActiveSwapToDecimal:   "18",
	}

	data, err := ReadSwapPreviewData(ctx, store, sessionId)

	assert.NoError(t, err)
	assert.Equal(t, expectedResult, data)
}

func TestGetSwapFromVoucherData(t *testing.T) {
	ctx := context.Background()

	db := memdb.NewMemDb()
	err := db.Connect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	prefix := storedb.ToBytes(visedb.DATATYPE_USERDATA)
	spdb := storedb.NewSubPrefixDb(db, prefix)

	// Test pool swap data
	mockData := map[storedb.DataTyp][]byte{
		storedb.DATA_POOL_FROM_SYMBOLS:   []byte("1:AMANI\n2:AMUA"),
		storedb.DATA_POOL_FROM_BALANCES:  []byte("1:\n2:"),
		storedb.DATA_POOL_FROM_DECIMALS:  []byte("1:6\n2:4"),
		storedb.DATA_POOL_FROM_ADDRESSES: []byte("1:0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe\n2:0xF0C3C7581b8b96B59a97daEc8Bd48247cE078674"),
	}

	// Put the data
	for key, value := range mockData {
		err = spdb.Put(ctx, []byte(storedb.ToBytes(key)), []byte(value))
		if err != nil {
			t.Fatal(err)
		}
	}

	result, err := GetSwapFromVoucherData(ctx, spdb, "1")

	assert.NoError(t, err)
	assert.Equal(t, "AMANI", result.TokenSymbol)
	assert.Equal(t, "", result.Balance)
	assert.Equal(t, "6", result.TokenDecimals)
	assert.Equal(t, "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe", result.ContractAddress)
}

func TestUpdateSwapFromVoucherData(t *testing.T) {
	ctx, store := InitializeTestDb(t)
	sessionId := "session123"

	// New swap from voucher data
	newData := &dataserviceapi.TokenHoldings{
		TokenSymbol:     "AMANI",
		TokenDecimals:   "6",
		ContractAddress: "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe",
	}

	// Old temporary data
	tempData := &dataserviceapi.TokenHoldings{
		TokenSymbol:     "OLD",
		TokenDecimals:   "8",
		ContractAddress: "0xold",
	}
	require.NoError(t, StoreTemporaryVoucher(ctx, store, sessionId, tempData))

	// Execute update
	err := UpdateSwapFromVoucherData(ctx, store, sessionId, newData)
	require.NoError(t, err)

	// Verify active swap from data was stored correctly
	activeEntries := map[storedb.DataTyp][]byte{
		storedb.DATA_ACTIVE_SWAP_FROM_SYM:     []byte(newData.TokenSymbol),
		storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL: []byte(newData.TokenDecimals),
		storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS: []byte(newData.ContractAddress),
	}

	for key, expectedValue := range activeEntries {
		storedValue, err := store.ReadEntry(ctx, sessionId, key)
		require.NoError(t, err)
		require.Equal(t, expectedValue, storedValue, "Active swap from data mismatch for key %v", key)
	}
}

func TestGetSwapToVoucherData(t *testing.T) {
	ctx := context.Background()

	db := memdb.NewMemDb()
	err := db.Connect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	prefix := storedb.ToBytes(visedb.DATATYPE_USERDATA)
	spdb := storedb.NewSubPrefixDb(db, prefix)

	// Test pool swap data
	mockData := map[storedb.DataTyp][]byte{
		storedb.DATA_POOL_TO_SYMBOLS:   []byte("1:cUSD\n2:AMUA"),
		storedb.DATA_POOL_TO_BALANCES:  []byte("1:\n2:"),
		storedb.DATA_POOL_TO_DECIMALS:  []byte("1:6\n2:4"),
		storedb.DATA_POOL_TO_ADDRESSES: []byte("1:0xc7B78Ac9ACB9E025C8234621\n2:0xF0C3C7581b8b96B59a97daEc8Bd48247cE078674"),
	}

	// Put the data
	for key, value := range mockData {
		err = spdb.Put(ctx, []byte(storedb.ToBytes(key)), []byte(value))
		if err != nil {
			t.Fatal(err)
		}
	}

	result, err := GetSwapToVoucherData(ctx, spdb, "1")

	assert.NoError(t, err)
	assert.Equal(t, "cUSD", result.TokenSymbol)
	assert.Equal(t, "", result.Balance)
	assert.Equal(t, "6", result.TokenDecimals)
	assert.Equal(t, "0xc7B78Ac9ACB9E025C8234621", result.ContractAddress)
}