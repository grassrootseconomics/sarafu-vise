package store

import (
	"testing"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
)

func TestReadSwapData(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"
	ctx, store := InitializeTestDb(t)

	// Test swap data
	transactionData := map[storedb.DataTyp]string{
		storedb.DATA_PUBLIC_KEY:               publicKey,
		storedb.DATA_ACTIVE_POOL_ADDRESS:      "0x48a953cA5cf5298bc6f6Af3C608351f537AAcb9e",
		storedb.DATA_ACTIVE_SWAP_FROM_SYM:     "AMANI",
		storedb.DATA_ACTIVE_SWAP_FROM_DECIMAL: "6",
		storedb.DATA_ACTIVE_SWAP_FROM_ADDRESS: "0xc7B78Ac9ACB9E025C8234621FC515bC58179dEAe",
		storedb.DATA_ACTIVE_SWAP_TO_SYM:       "cUSD",
		storedb.DATA_ACTIVE_SWAP_TO_ADDRESS:   "0x765DE816845861e75A25fCA122bb6898B8B1282a",
	}

	// Store the data
	for key, value := range transactionData {
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
