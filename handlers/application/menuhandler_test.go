package application

import (
	"context"
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
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/testservice"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"

	"github.com/alecthomas/assert/v2"

	testdataloader "github.com/peteole/testdata-loader"
	"github.com/stretchr/testify/require"

	visedb "git.defalsify.org/vise.git/db"
	memdb "git.defalsify.org/vise.git/db/mem"
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
