package application

import (
	"context"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"testing"

	"git.defalsify.org/vise.git/cache"
	"git.defalsify.org/vise.git/persist"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"
	"git.grassecon.net/grassrootseconomics/common/pin"
	"git.grassecon.net/grassrootseconomics/sarafu-api/models"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/testservice"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"

	"github.com/alecthomas/assert/v2"

	testdataloader "github.com/peteole/testdata-loader"
	"github.com/stretchr/testify/require"

	visedb "git.defalsify.org/vise.git/db"
	memdb "git.defalsify.org/vise.git/db/mem"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
)

var (
	baseDir   = testdataloader.GetBasePath()
	flagsPath = path.Join(baseDir, "services", "registration", "pp.csv")
)

// mockReplaceSeparator function
var mockReplaceSeparator = func(input string) string {
	return strings.ReplaceAll(input, ":", ": ")
}

// InitializeTestStore sets up and returns an in-memory database and store.
func InitializeTestStore(t *testing.T) (context.Context, *store.UserDataStore) {
	ctx := context.Background()

	// Initialize memDb
	db := memdb.NewMemDb()
	err := db.Connect(ctx, "")
	require.NoError(t, err, "Failed to connect to memDb")

	// Create UserDataStore with memDb
	store := &store.UserDataStore{Db: db}

	t.Cleanup(func() {
		db.Close(ctx) // Ensure the DB is closed after each test
	})

	return ctx, store
}

// InitializeTestLogdbStore sets up and returns an in-memory database and logdb store.
func InitializeTestLogdbStore(t *testing.T) (context.Context, *store.UserDataStore) {
	ctx := context.Background()

	// Initialize memDb
	db := memdb.NewMemDb()
	err := db.Connect(ctx, "")
	require.NoError(t, err, "Failed to connect to memDb")

	// Create UserDataStore with memDb
	logdb := &store.UserDataStore{Db: db}

	t.Cleanup(func() {
		db.Close(ctx) // Ensure the DB is closed after each test
	})

	return ctx, logdb
}

func InitializeTestSubPrefixDb(t *testing.T, ctx context.Context) *storedb.SubPrefixDb {
	db := memdb.NewMemDb()
	err := db.Connect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	prefix := storedb.ToBytes(visedb.DATATYPE_USERDATA)
	spdb := storedb.NewSubPrefixDb(db, prefix)

	return spdb
}

func TestNewMenuHandlers(t *testing.T) {
	_, store := InitializeTestStore(t)
	_, logdb := InitializeTestLogdbStore(t)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	accountService := testservice.TestAccountService{}

	// Test case for valid UserDataStore
	t.Run("Valid UserDataStore", func(t *testing.T) {
		handlers, err := NewMenuHandlers(fm, store, logdb, &accountService, mockReplaceSeparator)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if handlers == nil {
			t.Fatal("expected handlers to be non-nil")
		}
		if handlers.userdataStore == nil {
			t.Fatal("expected userdataStore to be set in handlers")
		}
		if handlers.ReplaceSeparatorFunc == nil {
			t.Fatal("expected ReplaceSeparatorFunc to be set in handlers")
		}

		// Test ReplaceSeparatorFunc functionality
		input := "1:Menu item"
		expectedOutput := "1: Menu item"
		if handlers.ReplaceSeparatorFunc(input) != expectedOutput {
			t.Fatalf("ReplaceSeparatorFunc function did not return expected output: got %v, want %v", handlers.ReplaceSeparatorFunc(input), expectedOutput)
		}
	})

	// Test case for nil UserDataStore
	t.Run("Nil UserDataStore", func(t *testing.T) {
		handlers, err := NewMenuHandlers(fm, nil, logdb, &accountService, mockReplaceSeparator)
		if err == nil {
			t.Fatal("expected an error, got none")
		}
		if handlers != nil {
			t.Fatal("expected handlers to be nil")
		}
		expectedError := "cannot create handler with nil userdata store"
		if err.Error() != expectedError {
			t.Fatalf("expected error '%s', got '%v'", expectedError, err)
		}
	})
}

func TestInit(t *testing.T) {
	sessionId := "session123"
	ctx, testStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err.Error())
	}

	st := state.NewState(128)
	ca := cache.NewCache()

	tests := []struct {
		name           string
		setup          func() (*MenuHandlers, context.Context)
		input          []byte
		expectedResult resource.Result
	}{
		{
			name: "Handler not ready",
			setup: func() (*MenuHandlers, context.Context) {
				return &MenuHandlers{}, ctx
			},
			input:          []byte("1"),
			expectedResult: resource.Result{},
		},
		{
			name: "State and memory initialization",
			setup: func() (*MenuHandlers, context.Context) {
				pe := persist.NewPersister(testStore).WithSession(sessionId).WithContent(st, ca)
				h := &MenuHandlers{
					flagManager: fm,
					pe:          pe,
				}
				return h, context.WithValue(ctx, "SessionId", sessionId)
			},
			input:          []byte("1"),
			expectedResult: resource.Result{},
		},
		{
			name: "Non-admin session initialization",
			setup: func() (*MenuHandlers, context.Context) {
				pe := persist.NewPersister(testStore).WithSession("0712345678").WithContent(st, ca)
				h := &MenuHandlers{
					flagManager: fm,
					pe:          pe,
				}
				return h, context.WithValue(context.Background(), "SessionId", "0712345678")
			},
			input:          []byte("1"),
			expectedResult: resource.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, testCtx := tt.setup()
			res, err := h.Init(testCtx, "", tt.input)

			assert.NoError(t, err, "Unexpected error occurred")
			assert.Equal(t, res, tt.expectedResult, "Expected result should match actual result")
		})
	}
}

func TestWithPersister(t *testing.T) {
	// Test case: Setting a persister
	h := &MenuHandlers{}
	p := &persist.Persister{}

	h.SetPersister(p)

	assert.Equal(t, p, h.pe, "The persister should be set correctly.")
}

func TestWithPersister_PanicWhenAlreadySet(t *testing.T) {
	// Test case: Panic on multiple calls
	h := &MenuHandlers{pe: &persist.Persister{}}
	require.Panics(t, func() {
		h.SetPersister(&persist.Persister{})
	}, "Should panic when trying to set a persister again.")
}

func TestCheckIdentifier(t *testing.T) {
	sessionId := "session123"
	ctx, userdatastore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	_, logdb := InitializeTestLogdbStore(t)

	logDb := store.LogDb{
		Db: logdb,
	}

	// Define test cases
	tests := []struct {
		name            string
		publicKey       []byte
		mockErr         error
		expectedContent string
		expectError     bool
	}{
		{
			name:            "Saved public Key",
			publicKey:       []byte("0xa8363"),
			mockErr:         nil,
			expectedContent: "0xa8363",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := userdatastore.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(tt.publicKey))
			if err != nil {
				t.Fatal(err)
			}

			// Create the MenuHandlers instance with the mock store
			h := &MenuHandlers{
				userdataStore: userdatastore,
				logDb:         logDb,
			}

			// Call the method
			res, err := h.CheckIdentifier(ctx, "check_identifier", nil)

			// Assert results
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedContent, res.Content)
		})
	}
}

func TestGetFlag(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	expectedFlag := uint32(9)
	if err != nil {
		t.Logf(err.Error())
	}
	flag, err := fm.GetFlag("flag_account_created")
	if err != nil {
		t.Logf(err.Error())
	}

	assert.Equal(t, uint32(flag), expectedFlag, "Flags should be equal to account created")
}

func TestIncorrectPinReset(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	fm, err := NewFlagManager(flagsPath)

	if err != nil {
		log.Fatal(err)
	}

	flag_incorrect_pin, _ := fm.GetFlag("flag_incorrect_pin")
	flag_account_blocked, _ := fm.GetFlag("flag_account_blocked")

	ctx = context.WithValue(ctx, "SessionId", sessionId)

	// Define test cases
	tests := []struct {
		name           string
		input          []byte
		attempts       uint8
		expectedResult resource.Result
	}{
		{
			name:  "Test when incorrect PIN attempts is 2",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_pin},
				Content:   "1", //Expected remaining PIN attempts
			},
			attempts: 2,
		},
		{
			name:  "Test incorrect pin reset when incorrect PIN attempts is 1",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_pin},
				Content:   "2", //Expected remaining PIN attempts
			},
			attempts: 1,
		},
		{
			name:  "Test incorrect pin reset when incorrect PIN attempts is 1",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_pin},
				Content:   "2", //Expected remaining PIN attempts
			},
			attempts: 1,
		},
		{
			name:  "Test incorrect pin reset when incorrect PIN attempts is 3(account expected to be blocked)",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_pin},
				FlagSet:   []uint32{flag_account_blocked},
			},
			attempts: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if err := store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(strconv.Itoa(int(tt.attempts)))); err != nil {
				t.Fatal(err)
			}

			// Create the MenuHandlers instance with the mock flag manager
			h := &MenuHandlers{
				flagManager:   fm,
				userdataStore: store,
			}

			// Call the method
			res, err := h.ResetIncorrectPin(ctx, "reset_incorrect_pin", tt.input)
			if err != nil {
				t.Error(err)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should contain flag(s) that have been reset")
		})
	}
}

func TestVerifyCreatePin(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	// Create required mocks
	mockAccountService := new(mocks.MockAccountService)
	mockState := state.NewState(16)

	flag_valid_pin, _ := fm.GetFlag("flag_valid_pin")
	flag_pin_mismatch, _ := fm.GetFlag("flag_pin_mismatch")
	flag_pin_set, _ := fm.GetFlag("flag_pin_set")

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		flagManager:    fm,
		st:             mockState,
	}

	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Test with correct PIN confirmation",
			input: []byte("1234"),
			expectedResult: resource.Result{
				FlagSet:   []uint32{flag_valid_pin, flag_pin_set},
				FlagReset: []uint32{flag_pin_mismatch},
			},
		},
		{
			name:  "Test with PIN that does not match first ",
			input: []byte("1324"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_pin_mismatch},
			},
		},
	}

	// Hash the correct PIN
	hashedPIN, err := pin.HashPIN("1234")
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to hash temporaryPin", "error", err)
		t.Fatal(err)
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the method under test
			res, err := h.VerifyCreatePin(ctx, "verify_create_pin", []byte(tt.input))

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestQuit(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_account_authorized, _ := fm.GetFlag("flag_account_authorized")

	mockAccountService := new(mocks.MockAccountService)

	sessionId := "session123"

	ctx := context.WithValue(context.Background(), "SessionId", sessionId)

	h := &MenuHandlers{
		accountService: mockAccountService,
		flagManager:    fm,
	}
	tests := []struct {
		name           string
		input          []byte
		status         string
		expectedResult resource.Result
	}{
		{
			name: "Test quit message",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_authorized},
				Content:   "Thank you for using Sarafu. Goodbye!",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// Call the method under test
			res, _ := h.Quit(ctx, "test_quit", tt.input)

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestManageVouchers(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	_, logdb := InitializeTestLogdbStore(t)

	logDb := store.LogDb{
		Db: logdb,
	}

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
			expectedVoucherSymbols: []byte("1:TOKEN1"),
			expectedUpdatedAddress: []byte("0x123"),
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
			expectedVoucherSymbols: []byte("1:SRF\n2:MILO"),
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
				logDb:          logDb,
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
		"Name: %s\nSymbol: %s\nCommodity: %s\nLocation: %s", tokenDetails.TokenName, tokenDetails.TokenSymbol, tokenDetails.TokenCommodity, tokenDetails.TokenLocation,
	)
	mockAccountService.On("VoucherData", string(tokA_AAddress)).Return(tokenDetails, nil)
	res, err := h.GetVoucherDetails(ctx, "SessionId", []byte(""))
	expectedResult.FlagReset = append(expectedResult.FlagReset, flag_api_error)
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)
}

func TestRetrieveBlockedNumber(t *testing.T) {
	sessionId := "session123"
	blockedNumber := "0712345678"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	h := &MenuHandlers{
		userdataStore: userStore,
	}

	err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER, []byte(blockedNumber))
	if err != nil {
		t.Fatal(err)
	}

	res, err := h.RetrieveBlockedNumber(ctx, "retrieve_blocked_number", []byte(""))

	assert.NoError(t, err)

	assert.Equal(t, blockedNumber, res.Content)
}

func TestQuitWithHelp(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_account_authorized, _ := fm.GetFlag("flag_account_authorized")

	sessionId := "session123"

	ctx := context.WithValue(context.Background(), "SessionId", sessionId)

	h := &MenuHandlers{
		flagManager: fm,
	}
	tests := []struct {
		name           string
		input          []byte
		status         string
		expectedResult resource.Result
	}{
		{
			name: "Test quit with help message",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_authorized},
				Content:   "For more help, please call: 0757628885",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := h.QuitWithHelp(ctx, "quit_with_help", tt.input)
			//Assert that the aflag_account_authorized has been reset to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestShowBlockedAccount(t *testing.T) {
	sessionId := "session123"
	ctx := context.WithValue(context.Background(), "SessionId", sessionId)

	h := &MenuHandlers{}

	tests := []struct {
		name           string
		input          []byte
		status         string
		expectedResult resource.Result
	}{
		{
			name: "Test quit with Show Blocked Account",
			expectedResult: resource.Result{
				Content: "Your account has been locked. For help on how to unblock your account, contact support at: 0757628885",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := h.ShowBlockedAccount(ctx, "show_blocked_account", tt.input)
			//Assert that the result is as expected
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestResetUnregisteredNumber(t *testing.T) {
	ctx := context.Background()

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	flag_unregistered_number, _ := fm.GetFlag("flag_unregistered_number")

	expectedResult := resource.Result{
		FlagReset: []uint32{flag_unregistered_number},
	}

	h := &MenuHandlers{
		flagManager: fm,
	}

	res, err := h.ResetUnregisteredNumber(ctx, "reset_unregistered_number", []byte(""))

	assert.NoError(t, err)

	assert.Equal(t, expectedResult, res)
}

func TestClearTemporaryValue(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"

	ctx = context.WithValue(ctx, "SessionId", sessionId)

	h := &MenuHandlers{
		userdataStore: store,
	}

	// Write initial data to the store
	err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte("SomePreviousDATA34$"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = h.ClearTemporaryValue(ctx, "clear_temporary_value", []byte(""))

	assert.NoError(t, err)
	// Read current temp value from the store
	currentTempValue, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		t.Fatal(err)
	}

	// assert that the temp value is empty
	assert.Equal(t, currentTempValue, []byte(""))
}
