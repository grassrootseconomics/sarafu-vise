package application

import (
	"context"
	"fmt"
	"testing"

	"git.grassecon.net/grassrootseconomics/sarafu-api/models"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
	"github.com/grassrootseconomics/go-vise/resource"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

func TestManageVouchers(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	flag_no_active_voucher, err := fm.GetFlag("flag_no_active_voucher")
	if err != nil {
		t.Fatal(err)
	}
	flag_api_error, err := fm.GetFlag("flag_api_call_error")

	if err != nil {
		t.Fatal(err)
	}

	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name                   string
		vouchersResp           []dataserviceapi.TokenHoldings
		storedActiveVoucher    string
		expectedVoucherSymbols []byte
		expectedUpdatedAddress []byte
		expectedResult         resource.Result
	}{
		{
			name:                   "No vouchers available",
			vouchersResp:           []dataserviceapi.TokenHoldings{},
			expectedVoucherSymbols: []byte(""),
			expectedUpdatedAddress: []byte(""),
			expectedResult: resource.Result{
				FlagSet:   []uint32{flag_no_active_voucher},
				FlagReset: []uint32{flag_api_error},
			},
		},
		{
			name: "Set default voucher when no active voucher is set",
			vouchersResp: []dataserviceapi.TokenHoldings{
				{
					TokenAddress:  "0x123",
					TokenSymbol:   "TOKEN1",
					TokenDecimals: "18",
					Balance:       "100",
				},
			},
			expectedVoucherSymbols: []byte(""),
			expectedUpdatedAddress: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_api_error, flag_no_active_voucher},
			},
		},
		{
			name: "Check and update active voucher balance",
			vouchersResp: []dataserviceapi.TokenHoldings{
				{TokenAddress: "0xd4c288865Ce", TokenSymbol: "SRF", TokenDecimals: "6", Balance: "100"},
				{TokenAddress: "0x41c188d63Qa", TokenSymbol: "MILO", TokenDecimals: "4", Balance: "200"},
			},
			storedActiveVoucher:    "SRF",
			expectedVoucherSymbols: []byte("1:MILO"),
			expectedUpdatedAddress: []byte("0xd4c288865Ce"),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_api_error, flag_no_active_voucher},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)

			h := &MenuHandlers{
				userdataStore:  userStore,
				accountService: mockAccountService,
				flagManager:    fm,
			}

			mockAccountService.On("FetchVouchers", string(publicKey)).Return(tt.vouchersResp, nil)

			// Store active voucher if needed
			if tt.storedActiveVoucher != "" {
				err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM, []byte(tt.storedActiveVoucher))
				if err != nil {
					t.Fatal(err)
				}
				err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS, []byte("0x41c188D45rfg6ds"))
				if err != nil {
					t.Fatal(err)
				}
			}

			res, err := h.ManageVouchers(ctx, "manage_vouchers", []byte(""))
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResult, res)

			if tt.storedActiveVoucher != "" {
				// Validate stored voucher symbols
				voucherData, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_VOUCHER_SYMBOLS)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedVoucherSymbols, voucherData)

				// Validate stored active contract address
				updatedAddress, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedUpdatedAddress, updatedAddress)

				mockAccountService.AssertExpectations(t)
			}
		})
	}
}

func TestGetVoucherList(t *testing.T) {
	sessionId := "session123"

	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	// Initialize MenuHandlers
	h := &MenuHandlers{
		userdataStore:        store,
		ReplaceSeparatorFunc: mockReplaceSeparator,
	}

	mockSymbols := []byte("1:SRF\n2:MILO")
	mockBalances := []byte("1:10.099999\n2:40.7")

	// Put voucher symnols and balances data to the store
	err := store.WriteEntry(ctx, sessionId, storedb.DATA_VOUCHER_SYMBOLS, mockSymbols)
	if err != nil {
		t.Fatal(err)
	}
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_VOUCHER_BALANCES, mockBalances)
	if err != nil {
		t.Fatal(err)
	}

	expectedList := []byte("1: SRF 10.09\n2: MILO 40.70")

	res, err := h.GetVoucherList(ctx, "", []byte(""))

	assert.NoError(t, err)
	assert.Equal(t, res.Content, string(expectedList))
}

func TestViewVoucher(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
	}

	// Define mock voucher data
	mockData := map[storedb.DataTyp][]byte{
		storedb.DATA_VOUCHER_SYMBOLS:   []byte("1:SRF\n2:MILO"),
		storedb.DATA_VOUCHER_BALANCES:  []byte("1:100\n2:200"),
		storedb.DATA_VOUCHER_DECIMALS:  []byte("1:6\n2:4"),
		storedb.DATA_VOUCHER_ADDRESSES: []byte("1:0xd4c288865Ce\n2:0x41c188d63Qa"),
	}

	// Put the data
	for key, value := range mockData {
		err := store.WriteEntry(ctx, sessionId, key, []byte(value))
		if err != nil {
			t.Fatal(err)
		}
	}

	res, err := h.ViewVoucher(ctx, "view_voucher", []byte("1"))
	assert.NoError(t, err)
	assert.Equal(t, res.Content, "Symbol: SRF\nBalance: 100")
}

func TestSetVoucher(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"

	ctx = context.WithValue(ctx, "SessionId", sessionId)

	h := &MenuHandlers{
		userdataStore: store,
	}

	// Define the temporary voucher data
	tempData := &dataserviceapi.TokenHoldings{
		TokenSymbol:   "SRF",
		Balance:       "200",
		TokenDecimals: "6",
		TokenAddress:  "0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9",
	}

	expectedData := fmt.Sprintf("%s,%s,%s,%s", tempData.TokenSymbol, tempData.Balance, tempData.TokenDecimals, tempData.TokenAddress)

	// store the expectedData
	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(expectedData)); err != nil {
		t.Fatal(err)
	}

	res, err := h.SetVoucher(ctx, "set_voucher", []byte(""))

	assert.NoError(t, err)

	assert.Equal(t, string(tempData.TokenSymbol), res.Content)
}

func TestGetVoucherDetails(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	flag_api_error, _ := fm.GetFlag("flag_api_call_error")
	mockAccountService := new(mocks.MockAccountService)

	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	expectedResult := resource.Result{}

	tokA_AAddress := "0x0000000000000000000000000000000000000000"

	h := &MenuHandlers{
		userdataStore:  store,
		flagManager:    fm,
		accountService: mockAccountService,
	}
	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS, []byte(tokA_AAddress))
	if err != nil {
		t.Fatal(err)
	}
	tokenDetails := &models.VoucherDataResult{
		TokenName:      "Token A",
		TokenSymbol:    "TOKA",
		TokenLocation:  "Kilifi,Kenya",
		TokenCommodity: "Farming",
	}
	expectedResult.Content = fmt.Sprintf(
		"Name: %s\nSymbol: %s\nProduct: %s\nLocation: %s", tokenDetails.TokenName, tokenDetails.TokenSymbol, tokenDetails.TokenCommodity, tokenDetails.TokenLocation,
	)
	mockAccountService.On("VoucherData", string(tokA_AAddress)).Return(tokenDetails, nil)
	res, err := h.GetVoucherDetails(ctx, "SessionId", []byte(""))
	expectedResult.FlagReset = append(expectedResult.FlagReset, flag_api_error)
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)
}
