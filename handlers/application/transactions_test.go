package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
	"github.com/grassrootseconomics/go-vise/resource"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

func TestCheckTransactions(t *testing.T) {
	mockAccountService := new(mocks.MockAccountService)
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	spdb := InitializeTestSubPrefixDb(t, ctx)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	h := &MenuHandlers{
		userdataStore:  userStore,
		accountService: mockAccountService,
		prefixDb:       spdb,
		flagManager:    fm,
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	mockTXResponse := []dataserviceapi.Last10TxResponse{
		{
			Sender: "0X13242618721", Recipient: "0x41c188d63Qa", TransferValue: "100", ContractAddress: "0X1324262343rfdGW23",
			TxHash: "0x123wefsf34rf", DateBlock: time.Now(), TokenSymbol: "SRF", TokenDecimals: "6",
		},
		{
			Sender: "0x41c188d63Qa", Recipient: "0X13242618721", TransferValue: "200", ContractAddress: "0X1324262343rfdGW23",
			TxHash: "0xq34wresfdb44", DateBlock: time.Now(), TokenSymbol: "SRF", TokenDecimals: "6",
		},
	}

	expectedSenders := []byte("0X13242618721\n0x41c188d63Qa")

	mockAccountService.On("FetchTransactions", string(publicKey)).Return(mockTXResponse, nil)

	_, err = h.CheckTransactions(ctx, "check_transactions", []byte(""))
	assert.NoError(t, err)

	// Read tranfers senders data from the store
	senderData, err := spdb.Get(ctx, storedb.ToBytes(storedb.DATA_TX_SENDERS))
	if err != nil {
		t.Fatal(err)
	}

	// assert that the data is stored correctly
	assert.Equal(t, expectedSenders, senderData)

	mockAccountService.AssertExpectations(t)
}

func TestGetTransactionsList(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	spdb := InitializeTestSubPrefixDb(t, ctx)

	// Initialize MenuHandlers
	h := &MenuHandlers{
		userdataStore:        userStore,
		prefixDb:             spdb,
		ReplaceSeparatorFunc: mockReplaceSeparator,
	}

	err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	dateBlock, err := time.Parse(time.RFC3339, "2024-10-03T07:23:12Z")
	if err != nil {
		t.Fatal(err)
	}

	mockTXResponse := []dataserviceapi.Last10TxResponse{
		{
			Sender: "0X13242618721", Recipient: "0x41c188d63Qa", TransferValue: "1000", ContractAddress: "0X1324262343rfdGW23",
			TxHash: "0x123wefsf34rf", DateBlock: dateBlock, TokenSymbol: "SRF", TokenDecimals: "2",
		},
		{
			Sender: "0x41c188d63Qa", Recipient: "0X13242618721", TransferValue: "2000", ContractAddress: "0X1324262343rfdGW23",
			TxHash: "0xq34wresfdb44", DateBlock: dateBlock, TokenSymbol: "SRF", TokenDecimals: "2",
		},
	}

	data := store.ProcessTransfers(mockTXResponse)

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
			t.Fatal(err)
		}
	}

	expectedTransactionList := []byte("1: Sent 10 SRF 2024-10-03\n2: Received 20 SRF 2024-10-03")

	res, err := h.GetTransactionsList(ctx, "", []byte(""))

	assert.NoError(t, err)
	assert.Equal(t, res.Content, string(expectedTransactionList))
}

func TestViewTransactionStatement(t *testing.T) {
	ctx, userStore := InitializeTestStore(t)
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx = context.WithValue(ctx, "SessionId", sessionId)
	spdb := InitializeTestSubPrefixDb(t, ctx)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_incorrect_statement, _ := fm.GetFlag("flag_incorrect_statement")

	h := &MenuHandlers{
		userdataStore: userStore,
		prefixDb:      spdb,
		flagManager:   fm,
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	dateBlock, err := time.Parse(time.RFC3339, "2024-10-03T07:23:12Z")
	if err != nil {
		t.Fatal(err)
	}

	mockTXResponse := []dataserviceapi.Last10TxResponse{
		{
			Sender: "0X13242618721", Recipient: "0x41c188d63Qa", TransferValue: "1000", ContractAddress: "0X1324262343rfdGW23",
			TxHash: "0x123wefsf34rf", DateBlock: dateBlock, TokenSymbol: "SRF", TokenDecimals: "2",
		},
		{
			Sender: "0x41c188d63Qa", Recipient: "0X13242618721", TransferValue: "2000", ContractAddress: "0X1324262343rfdGW23",
			TxHash: "0xq34wresfdb44", DateBlock: dateBlock, TokenSymbol: "SRF", TokenDecimals: "2",
		},
	}

	data := store.ProcessTransfers(mockTXResponse)

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
			t.Fatal(err)
		}
	}

	tests := []struct {
		name           string
		input          []byte
		expectedError  error
		expectedResult resource.Result
	}{
		{
			name:          "Valid input - index 1",
			input:         []byte("1"),
			expectedError: nil,
			expectedResult: resource.Result{
				Content:   "Sent 10 SRF\nTo: 0x41c188d63Qa\nContract address: 0X1324262343rfdGW23\nTxhash: 0x123wefsf34rf\nDate: 2024-10-03 07:23:12 AM",
				FlagReset: []uint32{flag_incorrect_statement},
			},
		},
		{
			name:          "Valid input - index 2",
			input:         []byte("2"),
			expectedError: nil,
			expectedResult: resource.Result{
				Content:   "Received 20 SRF\nFrom: 0x41c188d63Qa\nContract address: 0X1324262343rfdGW23\nTxhash: 0xq34wresfdb44\nDate: 2024-10-03 07:23:12 AM",
				FlagReset: []uint32{flag_incorrect_statement},
			},
		},
		{
			name:          "Invalid input - index 0",
			input:         []byte("0"),
			expectedError: nil,
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_statement},
			},
		},
		{
			name:           "Invalid input - index 12",
			input:          []byte("12"),
			expectedError:  fmt.Errorf("invalid input: index must be between 1 and 10"),
			expectedResult: resource.Result{},
		},
		{
			name:           "Invalid input - non-numeric",
			input:          []byte("abc"),
			expectedError:  fmt.Errorf("invalid input: must be a number between 1 and 10"),
			expectedResult: resource.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := h.ViewTransactionStatement(ctx, "view_transaction_statement", tt.input)

			if tt.expectedError != nil {
				assert.EqualError(t, err, tt.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedResult, res)
		})
	}
}
