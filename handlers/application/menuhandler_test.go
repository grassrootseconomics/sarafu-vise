package application

import (
	"context"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.defalsify.org/vise.git/cache"
	"git.defalsify.org/vise.git/lang"
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

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	accountService := testservice.TestAccountService{}

	// Test case for valid UserDataStore
	t.Run("Valid UserDataStore", func(t *testing.T) {
		handlers, err := NewMenuHandlers(fm, store, &accountService, mockReplaceSeparator)
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
		handlers, err := NewMenuHandlers(fm, nil, &accountService, mockReplaceSeparator)
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
		{
			name: "Move to top node on empty input",
			setup: func() (*MenuHandlers, context.Context) {
				pe := persist.NewPersister(testStore).WithSession(sessionId).WithContent(st, ca)
				h := &MenuHandlers{
					flagManager: fm,
					pe:          pe,
				}
				st.Code = []byte("some pending bytecode")
				return h, context.WithValue(ctx, "SessionId", sessionId)
			},
			input:          []byte(""),
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

func TestCreateAccount(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	flag_account_created, err := fm.GetFlag("flag_account_created")
	if err != nil {
		t.Logf(err.Error())
	}

	tests := []struct {
		name           string
		serverResponse *models.AccountResult
		expectedResult resource.Result
	}{
		{
			name: "Test account creation success",
			serverResponse: &models.AccountResult{
				TrackingId: "1234567890",
				PublicKey:  "0xD3adB33f",
			},
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_account_created},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)

			h := &MenuHandlers{
				userdataStore:  store,
				accountService: mockAccountService,
				flagManager:    fm,
			}

			mockAccountService.On("CreateAccount").Return(tt.serverResponse, nil)

			// Call the method you want to test
			res, err := h.CreateAccount(ctx, "create_account", []byte(""))

			// Assert that no errors occurred
			assert.NoError(t, err)

			// Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestWithPersister(t *testing.T) {
	// Test case: Setting a persister
	h := &MenuHandlers{}
	p := &persist.Persister{}

	h.SetPersister(p)

	assert.Equal(t, p, h.pe, "The persister should be set correctly.")
	//assert.Equal(t, h, result, "The returned handler should be the same instance.")
}

func TestWithPersister_PanicWhenAlreadySet(t *testing.T) {
	// Test case: Panic on multiple calls
	h := &MenuHandlers{pe: &persist.Persister{}}
	require.Panics(t, func() {
		h.SetPersister(&persist.Persister{})
	}, "Should panic when trying to set a persister again.")
}

func TestSaveFirstname(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")
	flag_firstname_set, _ := fm.GetFlag("flag_firstname_set")

	// Set the flag in the State
	mockState := state.NewState(128)
	mockState.SetFlag(flag_allow_update)

	expectedResult := resource.Result{}

	// Define test data
	firstName := "John"

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(firstName)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_firstname_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveFirstname(ctx, "save_firstname", []byte(firstName))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_FIRST_NAME entry has been updated with the temporary value
	storedFirstName, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME)
	assert.Equal(t, firstName, string(storedFirstName))
}

func TestSaveFamilyname(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")
	flag_firstname_set, _ := fm.GetFlag("flag_familyname_set")

	// Set the flag in the State
	mockState := state.NewState(128)
	mockState.SetFlag(flag_allow_update)

	expectedResult := resource.Result{}

	expectedResult.FlagSet = []uint32{flag_firstname_set}

	// Define test data
	familyName := "Doeee"

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(familyName)); err != nil {
		t.Fatal(err)
	}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
		st:            mockState,
		flagManager:   fm,
	}

	// Call the method
	res, err := h.SaveFamilyname(ctx, "save_familyname", []byte(familyName))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_FAMILY_NAME entry has been updated with the temporary value
	storedFamilyName, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME)
	assert.Equal(t, familyName, string(storedFamilyName))
}

func TestSaveYoB(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")
	flag_yob_set, _ := fm.GetFlag("flag_yob_set")

	// Set the flag in the State
	mockState := state.NewState(108)
	mockState.SetFlag(flag_allow_update)

	expectedResult := resource.Result{}

	// Define test data
	yob := "1980"

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(yob)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_yob_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveYob(ctx, "save_yob", []byte(yob))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_YOB entry has been updated with the temporary value
	storedYob, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_YOB)
	assert.Equal(t, yob, string(storedYob))
}

func TestSaveLocation(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")
	flag_location_set, _ := fm.GetFlag("flag_location_set")

	// Set the flag in the State
	mockState := state.NewState(108)
	mockState.SetFlag(flag_allow_update)

	expectedResult := resource.Result{}

	// Define test data
	location := "Kilifi"

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(location)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_location_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveLocation(ctx, "save_location", []byte(location))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_LOCATION entry has been updated with the temporary value
	storedLocation, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_LOCATION)
	assert.Equal(t, location, string(storedLocation))
}

func TestSaveOfferings(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")
	flag_offerings_set, _ := fm.GetFlag("flag_offerings_set")

	// Set the flag in the State
	mockState := state.NewState(108)
	mockState.SetFlag(flag_allow_update)

	expectedResult := resource.Result{}

	// Define test data
	offerings := "Bananas"

	if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(offerings)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_offerings_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveOfferings(ctx, "save_offerings", []byte(offerings))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_OFFERINGS entry has been updated with the temporary value
	storedOfferings, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_OFFERINGS)
	assert.Equal(t, offerings, string(storedOfferings))
}

func TestSaveGender(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")
	flag_gender_set, _ := fm.GetFlag("flag_gender_set")

	// Set the flag in the State
	mockState := state.NewState(108)
	mockState.SetFlag(flag_allow_update)

	// Define test cases
	tests := []struct {
		name            string
		input           []byte
		expectedGender  string
		executingSymbol string
	}{
		{
			name:            "Valid Male Input",
			input:           []byte("1"),
			expectedGender:  "male",
			executingSymbol: "set_male",
		},
		{
			name:            "Valid Female Input",
			input:           []byte("2"),
			expectedGender:  "female",
			executingSymbol: "set_female",
		},
		{
			name:            "Valid Unspecified Input",
			input:           []byte("3"),
			executingSymbol: "set_unspecified",
			expectedGender:  "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(tt.expectedGender)); err != nil {
				t.Fatal(err)
			}

			mockState.ExecPath = append(mockState.ExecPath, tt.executingSymbol)
			// Create the MenuHandlers instance with the mock store
			h := &MenuHandlers{
				userdataStore: store,
				st:            mockState,
				flagManager:   fm,
			}

			expectedResult := resource.Result{}

			// Call the method
			res, err := h.SaveGender(ctx, "save_gender", tt.input)

			expectedResult.FlagSet = []uint32{flag_gender_set}

			// Assert results
			assert.NoError(t, err)
			assert.Equal(t, expectedResult, res)

			// Verify that the DATA_GENDER entry has been updated with the temporary value
			storedGender, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_GENDER)
			assert.Equal(t, tt.expectedGender, string(storedGender))
		})
	}
}

func TestSaveTemporaryPin(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	flag_incorrect_pin, _ := fm.GetFlag("flag_incorrect_pin")

	// Create the MenuHandlers instance with the mock flag manager
	h := &MenuHandlers{
		flagManager:   fm,
		userdataStore: store,
	}

	// Define test cases
	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Valid Pin entry",
			input: []byte("1234"),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_pin},
			},
		},
		{
			name:  "Invalid Pin entry",
			input: []byte("12343"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_incorrect_pin},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the method
			res, err := h.SaveTemporaryPin(ctx, "save_pin", tt.input)

			if err != nil {
				t.Error(err)
			}
			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should match expected result")
		})
	}
}

func TestCheckIdentifier(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

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
			err := store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(tt.publicKey))
			if err != nil {
				t.Fatal(err)
			}

			// Create the MenuHandlers instance with the mock store
			h := &MenuHandlers{
				userdataStore: store,
			}

			// Call the method
			res, err := h.CheckIdentifier(ctx, "check_identifier", nil)

			// Assert results
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedContent, res.Content)
		})
	}
}

func TestGetSender(t *testing.T) {
	sessionId := "session123"
	ctx, _ := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	// Create the MenuHandlers instance
	h := &MenuHandlers{}

	// Call the method
	res, _ := h.GetSender(ctx, "get_sender", []byte(""))

	//Assert that the sessionId is what was set as the result content.
	assert.Equal(t, sessionId, res.Content)
}

func TestGetAmount(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	// Define test data
	amount := "0.03"
	activeSym := "SRF"

	err := store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(amount))
	if err != nil {
		t.Fatal(err)
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM, []byte(activeSym))
	if err != nil {
		t.Fatal(err)
	}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
	}

	// Call the method
	res, _ := h.GetAmount(ctx, "get_amount", []byte(""))

	formattedAmount := fmt.Sprintf("%s %s", amount, activeSym)

	//Assert that the retrieved amount is what was set as the content
	assert.Equal(t, formattedAmount, res.Content)
}

func TestGetRecipient(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	recepient := "0712345678"

	err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(recepient))
	if err != nil {
		t.Fatal(err)
	}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: store,
	}

	// Call the method
	res, _ := h.GetRecipient(ctx, "get_recipient", []byte(""))

	//Assert that the retrieved recepient is what was set as the content
	assert.Equal(t, recepient, res.Content)
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

func TestSetLanguage(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	sessionId := "session123"
	ctx, store := InitializeTestStore(t)

	ctx = context.WithValue(ctx, "SessionId", sessionId)

	// Define test cases
	tests := []struct {
		name           string
		execPath       []string
		expectedResult resource.Result
	}{
		{
			name:     "Set Default Language (English)",
			execPath: []string{"set_eng"},
			expectedResult: resource.Result{
				FlagSet: []uint32{state.FLAG_LANG, 8},
				Content: "eng",
			},
		},
		{
			name:     "Set Swahili Language",
			execPath: []string{"set_swa"},
			expectedResult: resource.Result{
				FlagSet: []uint32{state.FLAG_LANG, 8},
				Content: "swa",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockState := state.NewState(16)
			// Set the ExecPath
			mockState.ExecPath = tt.execPath

			// Create the MenuHandlers instance with the mock flag manager
			h := &MenuHandlers{
				flagManager:   fm,
				userdataStore: store,
				st:            mockState,
			}

			// Call the method
			res, err := h.SetLanguage(ctx, "set_language", nil)
			if err != nil {
				t.Error(err)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should match expected result")
			code, err := store.ReadEntry(ctx, sessionId, storedb.DATA_SELECTED_LANGUAGE_CODE)
			if err != nil {
				t.Error(err)
			}

			assert.Equal(t, string(code), tt.expectedResult.Content)
			code, err = store.ReadEntry(ctx, sessionId, storedb.DATA_INITIAL_LANGUAGE_CODE)
			if err != nil {
				t.Error(err)
			}
			assert.Equal(t, string(code), "eng")
		})
	}
}

func TestResetAllowUpdate(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	flag_allow_update, _ := fm.GetFlag("flag_allow_update")

	// Define test cases
	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Resets allow update",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_allow_update},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the MenuHandlers instance with the mock flag manager
			h := &MenuHandlers{
				flagManager: fm,
			}

			// Call the method
			res, err := h.ResetAllowUpdate(context.Background(), "reset_allow update", tt.input)
			if err != nil {
				t.Error(err)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Flags should be equal to account created")
		})
	}
}

func TestResetAccountAuthorized(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	flag_account_authorized, _ := fm.GetFlag("flag_account_authorized")

	// Define test cases
	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Resets account authorized",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_authorized},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the MenuHandlers instance with the mock flag manager
			h := &MenuHandlers{
				flagManager: fm,
			}

			// Call the method
			res, err := h.ResetAccountAuthorized(context.Background(), "reset_account_authorized", tt.input)
			if err != nil {
				t.Error(err)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should contain flag(s) that have been reset")
		})
	}
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

func TestResetIncorrectYob(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	flag_incorrect_date_format, _ := fm.GetFlag("flag_incorrect_date_format")

	// Define test cases
	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Test incorrect yob reset",
			input: []byte(""),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_date_format},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the MenuHandlers instance with the mock flag manager
			h := &MenuHandlers{
				flagManager: fm,
			}

			// Call the method
			res, err := h.ResetIncorrectYob(context.Background(), "reset_incorrect_yob", tt.input)
			if err != nil {
				t.Error(err)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should contain flag(s) that have been reset")
		})
	}
}

func TestAuthorize(t *testing.T) {
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
	flag_incorrect_pin, _ := fm.GetFlag("flag_incorrect_pin")
	flag_account_authorized, _ := fm.GetFlag("flag_account_authorized")
	flag_allow_update, _ := fm.GetFlag("flag_allow_update")

	// Set 1234 is the correct account pin
	accountPIN := "1234"

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
			name:  "Test with correct pin",
			input: []byte("1234"),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_pin},
				FlagSet:   []uint32{flag_allow_update, flag_account_authorized},
			},
		},
		{
			name:  "Test with incorrect pin",
			input: []byte("1235"),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_authorized},
				FlagSet:   []uint32{flag_incorrect_pin},
			},
		},
		{
			name:           "Test with pin that is not a 4 digit",
			input:          []byte("1235aqds"),
			expectedResult: resource.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hash the PIN
			hashedPIN, err := pin.HashPIN(accountPIN)
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to hash temporaryPin", "error", err)
				t.Fatal(err)
			}

			err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACCOUNT_PIN, []byte(hashedPIN))
			if err != nil {
				t.Fatal(err)
			}

			// Call the method under test
			res, err := h.Authorize(ctx, "authorize", []byte(tt.input))

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestVerifyYob(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	sessionId := "session123"
	// Create required mocks
	mockAccountService := new(mocks.MockAccountService)
	mockState := state.NewState(16)
	flag_incorrect_date_format, _ := fm.GetFlag("flag_incorrect_date_format")
	ctx := context.WithValue(context.Background(), "SessionId", sessionId)

	h := &MenuHandlers{
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
			name:  "Test with correct yob",
			input: []byte("1980"),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_incorrect_date_format},
			},
		},
		{
			name:  "Test with incorrect yob",
			input: []byte("sgahaha"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_incorrect_date_format},
			},
		},
		{
			name:  "Test with numeric but less 4 digits",
			input: []byte("123"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_incorrect_date_format},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the method under test
			res, err := h.VerifyYob(ctx, "verify_yob", []byte(tt.input))

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
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

func TestCheckAccountStatus(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_account_success, _ := fm.GetFlag("flag_account_success")
	flag_account_pending, _ := fm.GetFlag("flag_account_pending")
	flag_api_error, _ := fm.GetFlag("flag_api_call_error")

	tests := []struct {
		name           string
		publicKey      []byte
		response       *models.TrackStatusResult
		expectedResult resource.Result
	}{
		{
			name:      "Test when account is on the Sarafu network",
			publicKey: []byte("TrackingId1234"),
			response: &models.TrackStatusResult{
				Active: true,
			},
			expectedResult: resource.Result{
				FlagSet:   []uint32{flag_account_success},
				FlagReset: []uint32{flag_api_error, flag_account_pending},
			},
		},
		{
			name:      "Test when the account is not yet on the sarafu network",
			publicKey: []byte("TrackingId1234"),
			response: &models.TrackStatusResult{
				Active: false,
			},
			expectedResult: resource.Result{
				FlagSet:   []uint32{flag_account_pending},
				FlagReset: []uint32{flag_api_error, flag_account_success},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)

			h := &MenuHandlers{
				userdataStore:  store,
				accountService: mockAccountService,
				flagManager:    fm,
			}

			err = store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(tt.publicKey))
			if err != nil {
				t.Fatal(err)
			}

			mockAccountService.On("TrackAccountStatus", string(tt.publicKey)).Return(tt.response, nil)

			// Call the method under test
			res, _ := h.CheckAccountStatus(ctx, "check_account_status", []byte(""))

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestTransactionReset(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	flag_invalid_recipient, _ := fm.GetFlag("flag_invalid_recipient")
	flag_invalid_recipient_with_invite, _ := fm.GetFlag("flag_invalid_recipient_with_invite")

	mockAccountService := new(mocks.MockAccountService)

	h := &MenuHandlers{
		userdataStore:  store,
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
			name: "Test transaction reset for amount and recipient",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_invalid_recipient, flag_invalid_recipient_with_invite},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the method under test
			res, _ := h.TransactionReset(ctx, "transaction_reset", tt.input)

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestResetTransactionAmount(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	flag_invalid_amount, _ := fm.GetFlag("flag_invalid_amount")

	mockAccountService := new(mocks.MockAccountService)

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		flagManager:    fm,
	}

	tests := []struct {
		name           string
		expectedResult resource.Result
	}{
		{
			name: "Test amount reset",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_invalid_amount},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the method under test
			res, _ := h.ResetTransactionAmount(ctx, "transaction_reset_amount", []byte(""))

			// Assert that no errors occurred
			assert.NoError(t, err)

			//Assert that the account created flag has been set to the result
			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")
		})
	}
}

func TestInitiateTransaction(t *testing.T) {
	sessionId := "254712345678"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	account_authorized_flag, _ := fm.GetFlag("flag_account_authorized")

	mockAccountService := new(mocks.MockAccountService)

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		flagManager:    fm,
	}

	tests := []struct {
		name             string
		TemporaryValue   []byte
		ActiveSym        []byte
		StoredAmount     []byte
		TransferAmount   string
		PublicKey        []byte
		Recipient        []byte
		ActiveDecimal    []byte
		ActiveAddress    []byte
		TransferResponse *models.TokenTransferResponse
		expectedResult   resource.Result
	}{
		{
			name:           "Test initiate transaction",
			TemporaryValue: []byte("0711223344"),
			ActiveSym:      []byte("SRF"),
			StoredAmount:   []byte("1.00"),
			TransferAmount: "1000000",
			PublicKey:      []byte("0X13242618721"),
			Recipient:      []byte("0x12415ass27192"),
			ActiveDecimal:  []byte("6"),
			ActiveAddress:  []byte("0xd4c288865Ce"),
			TransferResponse: &models.TokenTransferResponse{
				TrackingId: "1234567890",
			},
			expectedResult: resource.Result{
				FlagReset: []uint32{account_authorized_flag},
				Content:   "Your request has been sent. 0711223344 will receive 1.00 SRF from 254712345678.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(tt.TemporaryValue))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM, []byte(tt.ActiveSym))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_AMOUNT, []byte(tt.StoredAmount))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(tt.PublicKey))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_RECIPIENT, []byte(tt.Recipient))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_DECIMAL, []byte(tt.ActiveDecimal))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS, []byte(tt.ActiveAddress))
			if err != nil {
				t.Fatal(err)
			}

			mockAccountService.On("TokenTransfer").Return(tt.TransferResponse, nil)

			// Call the method under test
			res, _ := h.InitiateTransaction(ctx, "transaction_reset_amount", []byte(""))

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

func TestValidateAmount(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	sessionId := "session123"

	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	flag_invalid_amount, _ := fm.GetFlag("flag_invalid_amount")

	mockAccountService := new(mocks.MockAccountService)

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		flagManager:    fm,
	}
	tests := []struct {
		name           string
		input          []byte
		activeBal      []byte
		balance        string
		expectedResult resource.Result
	}{
		{
			name:      "Test with valid amount",
			input:     []byte("4.10"),
			activeBal: []byte("5"),
			expectedResult: resource.Result{
				Content: "4.10",
			},
		},
		{
			name:      "Test with amount larger than active balance",
			input:     []byte("5.02"),
			activeBal: []byte("5"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_amount},
				Content: "5.02",
			},
		},
		{
			name:      "Test with invalid amount format",
			input:     []byte("0.02ms"),
			activeBal: []byte("5"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_amount},
				Content: "0.02ms",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.WriteEntry(ctx, sessionId, storedb.DATA_ACTIVE_BAL, []byte(tt.activeBal))
			if err != nil {
				t.Fatal(err)
			}

			// Call the method under test
			res, _ := h.ValidateAmount(ctx, "test_validate_amount", tt.input)

			// Assert no errors occurred
			assert.NoError(t, err)

			// Assert the result matches the expected result
			assert.Equal(t, tt.expectedResult, res, "Expected result should match actual result")
		})
	}
}

func TestValidateRecipient(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	sessionId := "session123"
	publicKey := "0X13242618721"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	flag_invalid_recipient, _ := fm.GetFlag("flag_invalid_recipient")
	flag_invalid_recipient_with_invite, _ := fm.GetFlag("flag_invalid_recipient_with_invite")

	// Define test cases
	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Test with invalid recepient",
			input: []byte("7?1234"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_recipient},
				Content: "7?1234",
			},
		},
		{
			name:  "Test with valid unregistered recepient",
			input: []byte("0712345678"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_recipient_with_invite},
				Content: "0712345678",
			},
		},
		{
			name:           "Test with valid registered recepient",
			input:          []byte("0711223344"),
			expectedResult: resource.Result{},
		},
		{
			name:           "Test with address",
			input:          []byte("0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9"),
			expectedResult: resource.Result{},
		},
		{
			name:           "Test with alias recepient",
			input:          []byte("alias123.sarafu.local"),
			expectedResult: resource.Result{},
		},
	}

	// store a public key for the valid recipient
	err = store.WriteEntry(ctx, "+254711223344", storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)
			// Create the MenuHandlers instance
			h := &MenuHandlers{
				flagManager:    fm,
				userdataStore:  store,
				accountService: mockAccountService,
			}

			aliasResponse := &models.AliasAddress{
				Address: "0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9",
			}

			mockAccountService.On("CheckAliasAddress", string(tt.input)).Return(aliasResponse, nil)

			// Call the method
			res, err := h.ValidateRecipient(ctx, "validate_recepient", tt.input)

			if err != nil {
				t.Error(err)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should contain flag(s) that have been reset")
		})
	}
}

func TestCheckBalance(t *testing.T) {
	ctx, store := InitializeTestStore(t)

	tests := []struct {
		name           string
		sessionId      string
		publicKey      string
		activeSym      string
		activeBal      string
		expectedResult resource.Result
		expectError    bool
	}{
		{
			name:           "User with active sym",
			sessionId:      "session123",
			publicKey:      "0X98765432109",
			activeSym:      "ETH",
			activeBal:      "1.5",
			expectedResult: resource.Result{Content: "Balance: 1.50 ETH\n"},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)
			ctx := context.WithValue(ctx, "SessionId", tt.sessionId)

			h := &MenuHandlers{
				userdataStore:  store,
				accountService: mockAccountService,
			}

			err := store.WriteEntry(ctx, tt.sessionId, storedb.DATA_ACTIVE_SYM, []byte(tt.activeSym))
			if err != nil {
				t.Fatal(err)
			}
			err = store.WriteEntry(ctx, tt.sessionId, storedb.DATA_ACTIVE_BAL, []byte(tt.activeBal))
			if err != nil {
				t.Fatal(err)
			}

			res, err := h.CheckBalance(ctx, "check_balance", []byte(""))

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedResult, res, "Result should match expected output")
			}

			mockAccountService.AssertExpectations(t)
		})
	}
}

func TestGetProfile(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)

	mockAccountService := new(mocks.MockAccountService)
	mockState := state.NewState(16)

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		st:             mockState,
	}

	tests := []struct {
		name         string
		languageCode string
		keys         []storedb.DataTyp
		profileInfo  []string
		result       resource.Result
	}{
		{
			name:         "Test with full profile information in eng",
			keys:         []storedb.DataTyp{storedb.DATA_FAMILY_NAME, storedb.DATA_FIRST_NAME, storedb.DATA_GENDER, storedb.DATA_OFFERINGS, storedb.DATA_LOCATION, storedb.DATA_YOB, storedb.DATA_ACCOUNT_ALIAS},
			profileInfo:  []string{"Doee", "John", "Male", "Bananas", "Kilifi", "1976", "DoeJohn"},
			languageCode: "eng",
			result: resource.Result{
				Content: fmt.Sprintf(
					"Name: %s\nGender: %s\nAge: %s\nLocation: %s\nYou provide: %s\nYour alias: %s\n",
					"John Doee", "Male", "49", "Kilifi", "Bananas", "DoeJohn",
				),
			},
		},
		{
			name:         "Test with with profile information in swa",
			keys:         []storedb.DataTyp{storedb.DATA_FAMILY_NAME, storedb.DATA_FIRST_NAME, storedb.DATA_GENDER, storedb.DATA_OFFERINGS, storedb.DATA_LOCATION, storedb.DATA_YOB, storedb.DATA_ACCOUNT_ALIAS},
			profileInfo:  []string{"Doee", "John", "Male", "Bananas", "Kilifi", "1976", "DoeJohn"},
			languageCode: "swa",
			result: resource.Result{
				Content: fmt.Sprintf(
					"Jina: %s\nJinsia: %s\nUmri: %s\nEneo: %s\nUnauza: %s\nLakabu yako: %s\n",
					"John Doee", "Male", "49", "Kilifi", "Bananas", "DoeJohn",
				),
			},
		},
		{
			name:         "Test with with profile information with language that is not yet supported",
			keys:         []storedb.DataTyp{storedb.DATA_FAMILY_NAME, storedb.DATA_FIRST_NAME, storedb.DATA_GENDER, storedb.DATA_OFFERINGS, storedb.DATA_LOCATION, storedb.DATA_YOB, storedb.DATA_ACCOUNT_ALIAS},
			profileInfo:  []string{"Doee", "John", "Male", "Bananas", "Kilifi", "1976", "DoeJohn"},
			languageCode: "nor",
			result: resource.Result{
				Content: fmt.Sprintf(
					"Name: %s\nGender: %s\nAge: %s\nLocation: %s\nYou provide: %s\nYour alias: %s\n",
					"John Doee", "Male", "49", "Kilifi", "Bananas", "DoeJohn",
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx = context.WithValue(ctx, "SessionId", sessionId)
			ctx = context.WithValue(ctx, "Language", lang.Language{
				Code: tt.languageCode,
			})
			for index, key := range tt.keys {
				err := store.WriteEntry(ctx, sessionId, key, []byte(tt.profileInfo[index]))
				if err != nil {
					t.Fatal(err)
				}
			}

			res, _ := h.GetProfileInfo(ctx, "get_profile_info", []byte(""))

			//Assert that the result set to content is what was expected
			assert.Equal(t, res, tt.result, "Result should contain profile information served back to user")
		})
	}
}

func TestVerifyNewPin(t *testing.T) {
	sessionId := "session123"

	fm, _ := NewFlagManager(flagsPath)

	flag_valid_pin, _ := fm.GetFlag("flag_valid_pin")
	mockAccountService := new(mocks.MockAccountService)
	h := &MenuHandlers{
		flagManager:    fm,
		accountService: mockAccountService,
	}
	ctx := context.WithValue(context.Background(), "SessionId", sessionId)

	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Test with valid pin",
			input: []byte("1234"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_valid_pin},
			},
		},
		{
			name:  "Test with invalid pin",
			input: []byte("123"),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_valid_pin},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//Call the function under test
			res, _ := h.VerifyNewPin(ctx, "verify_new_pin", tt.input)

			//Assert that the result set to content is what was expected
			assert.Equal(t, res, tt.expectedResult, "Result should contain flags set according to user input")
		})
	}
}

func TestConfirmPin(t *testing.T) {
	sessionId := "session123"

	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)
	flag_pin_mismatch, _ := fm.GetFlag("flag_pin_mismatch")
	mockAccountService := new(mocks.MockAccountService)
	h := &MenuHandlers{
		userdataStore:  store,
		flagManager:    fm,
		accountService: mockAccountService,
	}

	tests := []struct {
		name           string
		input          []byte
		temporarypin   string
		expectedResult resource.Result
	}{
		{
			name:         "Test with correct pin confirmation",
			input:        []byte("1234"),
			temporarypin: "1234",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_pin_mismatch},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hash the PIN
			hashedPIN, err := pin.HashPIN(tt.temporarypin)
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to hash temporaryPin", "error", err)
				t.Fatal(err)
			}

			// Set up the expected behavior of the mock
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
			if err != nil {
				t.Fatal(err)
			}

			//Call the function under test
			res, _ := h.ConfirmPinChange(ctx, "confirm_pin_change", tt.input)

			//Assert that the result set to content is what was expected
			assert.Equal(t, res, tt.expectedResult, "Result should contain flags set according to user input")

		})
	}
}

func TestFetchCommunityBalance(t *testing.T) {

	// Define test data
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)

	tests := []struct {
		name           string
		languageCode   string
		expectedResult resource.Result
	}{
		{
			name: "Test community balance content when language is english",
			expectedResult: resource.Result{
				Content: "Community Balance: 0.00",
			},
			languageCode: "eng",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			mockAccountService := new(mocks.MockAccountService)
			mockState := state.NewState(16)

			h := &MenuHandlers{
				userdataStore:  store,
				st:             mockState,
				accountService: mockAccountService,
			}
			ctx = context.WithValue(ctx, "SessionId", sessionId)
			ctx = context.WithValue(ctx, "Language", lang.Language{
				Code: tt.languageCode,
			})

			// Call the method
			res, _ := h.FetchCommunityBalance(ctx, "fetch_community_balance", []byte(""))

			//Assert that the result set to content is what was expected
			assert.Equal(t, res, tt.expectedResult, "Result should match expected result")
		})
	}
}

func TestSetDefaultVoucher(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_no_active_voucher, err := fm.GetFlag("flag_no_active_voucher")
	if err != nil {
		t.Logf(err.Error())
	}

	publicKey := "0X13242618721"

	tests := []struct {
		name           string
		vouchersResp   []dataserviceapi.TokenHoldings
		expectedResult resource.Result
	}{
		{
			name:         "Test no vouchers available",
			vouchersResp: []dataserviceapi.TokenHoldings{},
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_no_active_voucher},
			},
		},
		{
			name: "Test set default voucher when no active voucher is set",
			vouchersResp: []dataserviceapi.TokenHoldings{
				{
					ContractAddress: "0x123",
					TokenSymbol:     "TOKEN1",
					TokenDecimals:   "18",
					Balance:         "100",
				},
			},
			expectedResult: resource.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)

			h := &MenuHandlers{
				userdataStore:  store,
				accountService: mockAccountService,
				flagManager:    fm,
			}

			err := store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
			if err != nil {
				t.Fatal(err)
			}

			mockAccountService.On("FetchVouchers", string(publicKey)).Return(tt.vouchersResp, nil)

			res, err := h.SetDefaultVoucher(ctx, "set_default_voucher", []byte("some-input"))

			assert.NoError(t, err)

			assert.Equal(t, res, tt.expectedResult, "Expected result should be equal to the actual result")

			mockAccountService.AssertExpectations(t)
		})
	}
}

func TestCheckVouchers(t *testing.T) {
	mockAccountService := new(mocks.MockAccountService)
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	spdb := InitializeTestSubPrefixDb(t, ctx)

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		prefixDb:       spdb,
	}

	err := store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	mockVouchersResponse := []dataserviceapi.TokenHoldings{
		{ContractAddress: "0xd4c288865Ce", TokenSymbol: "SRF", TokenDecimals: "6", Balance: "100"},
		{ContractAddress: "0x41c188d63Qa", TokenSymbol: "MILO", TokenDecimals: "4", Balance: "200"},
	}

	expectedSym := []byte("1:SRF\n2:MILO")

	mockAccountService.On("FetchVouchers", string(publicKey)).Return(mockVouchersResponse, nil)

	_, err = h.CheckVouchers(ctx, "check_vouchers", []byte(""))
	assert.NoError(t, err)

	// Read voucher sym data from the store
	voucherData, err := spdb.Get(ctx, storedb.ToBytes(storedb.DATA_VOUCHER_SYMBOLS))
	if err != nil {
		t.Fatal(err)
	}

	// assert that the data is stored correctly
	assert.Equal(t, expectedSym, voucherData)

	mockAccountService.AssertExpectations(t)
}

func TestGetVoucherList(t *testing.T) {
	sessionId := "session123"

	ctx := context.WithValue(context.Background(), "SessionId", sessionId)

	spdb := InitializeTestSubPrefixDb(t, ctx)

	// Initialize MenuHandlers
	h := &MenuHandlers{
		prefixDb:             spdb,
		ReplaceSeparatorFunc: mockReplaceSeparator,
	}

	mockSyms := []byte("1:SRF\n2:MILO")

	// Put voucher sym data from the store
	err := spdb.Put(ctx, storedb.ToBytes(storedb.DATA_VOUCHER_SYMBOLS), mockSyms)
	if err != nil {
		t.Fatal(err)
	}

	expectedSyms := []byte("1: SRF\n2: MILO")

	res, err := h.GetVoucherList(ctx, "", []byte(""))

	assert.NoError(t, err)
	assert.Equal(t, res.Content, string(expectedSyms))
}

func TestViewVoucher(t *testing.T) {
	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"

	ctx = context.WithValue(ctx, "SessionId", sessionId)

	spdb := InitializeTestSubPrefixDb(t, ctx)

	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		prefixDb:      spdb,
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
		err = spdb.Put(ctx, []byte(storedb.ToBytes(key)), []byte(value))
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
		TokenSymbol:     "SRF",
		Balance:         "200",
		TokenDecimals:   "6",
		ContractAddress: "0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9",
	}

	expectedData := fmt.Sprintf("%s,%s,%s,%s", tempData.TokenSymbol, tempData.Balance, tempData.TokenDecimals, tempData.ContractAddress)

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
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)
}

func TestCountIncorrectPINAttempts(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	attempts := uint8(2)

	h := &MenuHandlers{
		userdataStore: store,
	}
	err := store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(strconv.Itoa(int(attempts))))
	if err != nil {
		t.Logf(err.Error())
	}
	err = h.incrementIncorrectPINAttempts(ctx, sessionId)
	if err != nil {
		t.Logf(err.Error())
	}

	attemptsAfterCount, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS)
	if err != nil {
		t.Logf(err.Error())
	}
	pinAttemptsValue, _ := strconv.ParseUint(string(attemptsAfterCount), 0, 64)
	pinAttemptsCount := uint8(pinAttemptsValue)
	expectedAttempts := attempts + 1
	assert.Equal(t, pinAttemptsCount, expectedAttempts)
}

func TestResetIncorrectPINAttempts(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	err := store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(string("2")))
	if err != nil {
		t.Logf(err.Error())
	}

	h := &MenuHandlers{
		userdataStore: store,
	}
	h.resetIncorrectPINAttempts(ctx, sessionId)
	incorrectAttempts, err := store.ReadEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS)

	if err != nil {
		t.Logf(err.Error())
	}
	assert.Equal(t, "0", string(incorrectAttempts))
}

func TestPersistLanguageCode(t *testing.T) {
	ctx, store := InitializeTestStore(t)

	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	h := &MenuHandlers{
		userdataStore: store,
	}
	tests := []struct {
		name                 string
		code                 string
		expectedLanguageCode string
	}{
		{
			name:                 "Set Default Language (English)",
			code:                 "eng",
			expectedLanguageCode: "eng",
		},
		{
			name:                 "Set Swahili Language",
			code:                 "swa",
			expectedLanguageCode: "swa",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := h.persistLanguageCode(ctx, test.code)
			if err != nil {
				t.Logf(err.Error())
			}
			code, err := store.ReadEntry(ctx, sessionId, storedb.DATA_SELECTED_LANGUAGE_CODE)

			assert.Equal(t, test.expectedLanguageCode, string(code))
		})
	}
}

func TestCheckBlockedStatus(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_account_blocked, err := fm.GetFlag("flag_account_blocked")
	if err != nil {
		t.Logf(err.Error())
	}

	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
	}

	tests := []struct {
		name                    string
		currentWrongPinAttempts string
		expectedResult          resource.Result
	}{
		{
			name:                    "Currently blocked account",
			currentWrongPinAttempts: "4",
			expectedResult:          resource.Result{},
		},
		{
			name:                    "Account with 0 wrong PIN attempts",
			currentWrongPinAttempts: "0",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_blocked},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.WriteEntry(ctx, sessionId, storedb.DATA_INCORRECT_PIN_ATTEMPTS, []byte(tt.currentWrongPinAttempts)); err != nil {
				t.Fatal(err)
			}

			res, err := h.CheckBlockedStatus(ctx, "", []byte(""))

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResult, res)
		})
	}
}

func TestPersistInitialLanguageCode(t *testing.T) {
	ctx, store := InitializeTestStore(t)

	h := &MenuHandlers{
		userdataStore: store,
	}

	tests := []struct {
		name      string
		code      string
		sessionId string
	}{
		{
			name:      "Persist initial Language (English)",
			code:      "eng",
			sessionId: "session123",
		},
		{
			name:      "Persist initial Language (Swahili)",
			code:      "swa",
			sessionId: "session456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.persistInitialLanguageCode(ctx, tt.sessionId, tt.code)
			if err != nil {
				t.Logf(err.Error())
			}
			code, err := store.ReadEntry(ctx, tt.sessionId, storedb.DATA_INITIAL_LANGUAGE_CODE)

			assert.Equal(t, tt.code, string(code))
		})
	}
}

func TestCheckTransactions(t *testing.T) {
	mockAccountService := new(mocks.MockAccountService)
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	spdb := InitializeTestSubPrefixDb(t, ctx)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	h := &MenuHandlers{
		userdataStore:  store,
		accountService: mockAccountService,
		prefixDb:       spdb,
		flagManager:    fm,
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
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

func TestMaxAmount(t *testing.T) {
	sessionId := "session123"
	activeBal := "500"

	tests := []struct {
		name           string
		sessionId      string
		activeBal      string
		expectedError  bool
		expectedResult resource.Result
	}{
		{
			name:           "Valid session ID and active balance",
			sessionId:      sessionId,
			activeBal:      activeBal,
			expectedError:  false,
			expectedResult: resource.Result{Content: activeBal},
		},
		{
			name:           "Missing Session ID",
			sessionId:      "",
			activeBal:      activeBal,
			expectedError:  true,
			expectedResult: resource.Result{},
		},
		{
			name:           "Failed to Read Active Balance",
			sessionId:      sessionId,
			activeBal:      "", // failure to read active balance
			expectedError:  true,
			expectedResult: resource.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, userStore := InitializeTestStore(t)
			if tt.sessionId != "" {
				ctx = context.WithValue(ctx, "SessionId", tt.sessionId)
			}

			h := &MenuHandlers{
				userdataStore: userStore,
			}

			// Write active balance to the store only if it's not empty
			if tt.activeBal != "" {
				err := userStore.WriteEntry(ctx, tt.sessionId, storedb.DATA_ACTIVE_BAL, []byte(tt.activeBal))
				if err != nil {
					t.Fatal(err)
				}
			}

			res, err := h.MaxAmount(ctx, "max_amount", []byte(""))

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedResult, res)
		})
	}
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

func TestValidateBlockedNumber(t *testing.T) {
	sessionId := "session123"
	validNumber := "+254712345678"
	invalidNumber := "12343"              // Invalid phone number
	unregisteredNumber := "+254734567890" // Valid but unregistered number
	publicKey := "0X13242618721"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	flag_unregistered_number, _ := fm.GetFlag("flag_unregistered_number")

	h := &MenuHandlers{
		userdataStore: userStore,
		flagManager:   fm,
	}

	err = userStore.WriteEntry(ctx, validNumber, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:           "Valid and registered number",
			input:          []byte(validNumber),
			expectedResult: resource.Result{},
		},
		{
			name:  "Invalid Phone Number",
			input: []byte(invalidNumber),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_unregistered_number},
			},
		},
		{
			name:  "Unregistered Phone Number",
			input: []byte(unregisteredNumber),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_unregistered_number},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := h.ValidateBlockedNumber(ctx, "validate_blocked_number", tt.input)

			assert.NoError(t, err)

			assert.Equal(t, tt.expectedResult, res)

			if tt.name == "Valid and registered number" {
				blockedNumber, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER)
				if err != nil {
					t.Fatal(err)
				}

				assert.Equal(t, validNumber, string(blockedNumber))
			}
		})
	}
}

func TestSaveOthersTemporaryPin(t *testing.T) {
	sessionId := "session123"
	blockedNumber := "+254712345678"
	testPin := "1234"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	h := &MenuHandlers{
		userdataStore: userStore,
	}

	tests := []struct {
		name          string
		sessionId     string
		blockedNumber string
		testPin       string
		setup         func() error // Setup function for each test case
		expectedError bool
		verifyResult  func(t *testing.T) // Function to verify the result
	}{
		{
			name:          "Missing Session ID",
			sessionId:     "", // Empty session ID
			blockedNumber: blockedNumber,
			testPin:       testPin,
			setup:         nil,
			expectedError: true,
			verifyResult:  nil,
		},
		{
			name:          "Failed to Read Blocked Number",
			sessionId:     sessionId,
			blockedNumber: blockedNumber,
			testPin:       testPin,
			setup: func() error {
				// Do not write the blocked number to simulate a read failure
				return nil
			},
			expectedError: true,
			verifyResult:  nil,
		},

		{
			name:          "Successfully save hashed PIN",
			sessionId:     sessionId,
			blockedNumber: blockedNumber,
			testPin:       testPin,
			setup: func() error {
				// Write the blocked number to the store
				return userStore.WriteEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER, []byte(blockedNumber))
			},
			expectedError: false,
			verifyResult: func(t *testing.T) {
				// Read the stored hashed PIN
				othersHashedPin, err := userStore.ReadEntry(ctx, blockedNumber, storedb.DATA_TEMPORARY_VALUE)
				if err != nil {
					t.Fatal(err)
				}

				// Verify that the stored hashed PIN matches the original PIN
				if !pin.VerifyPIN(string(othersHashedPin), testPin) {
					t.Fatal("stored hashed PIN does not match the original PIN")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the context with the session ID
			ctx := context.WithValue(context.Background(), "SessionId", tt.sessionId)

			// Run the setup function if provided
			if tt.setup != nil {
				err := tt.setup()
				if err != nil {
					t.Fatal(err)
				}
			}

			// Call the function under test
			_, err := h.SaveOthersTemporaryPin(ctx, "save_others_temporary_pin", []byte(tt.testPin))

			// Assert the error
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify the result if a verification function is provided
			if tt.verifyResult != nil {
				tt.verifyResult(t)
			}
		})
	}
}

func TestCheckBlockedNumPinMisMatch(t *testing.T) {
	sessionId := "session123"
	blockedNumber := "+254712345678"
	testPin := "1234"

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	hashedPIN, err := pin.HashPIN(testPin)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to hash testPin", "error", err)
		t.Fatal(err)
	}

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	flag_pin_mismatch, _ := fm.GetFlag("flag_pin_mismatch")

	h := &MenuHandlers{
		userdataStore: userStore,
		flagManager:   fm,
	}

	// Write initial data to the store
	err = userStore.WriteEntry(ctx, sessionId, storedb.DATA_BLOCKED_NUMBER, []byte(blockedNumber))
	if err != nil {
		t.Fatal(err)
	}
	err = userStore.WriteEntry(ctx, blockedNumber, storedb.DATA_TEMPORARY_VALUE, []byte(hashedPIN))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		input          []byte
		expectedResult resource.Result
	}{
		{
			name:  "Successful PIN match",
			input: []byte(testPin),
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_pin_mismatch},
			},
		},
		{
			name:  "PIN mismatch",
			input: []byte("1345"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_pin_mismatch},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := h.CheckBlockedNumPinMisMatch(ctx, "sym", tt.input)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedResult, res)
		})
	}
}

func TestGetCurrentProfileInfo(t *testing.T) {
	sessionId := "session123"
	ctx, store := InitializeTestStore(t)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	flag_firstname_set, _ := fm.GetFlag("flag_firstname_set")
	flag_familyname_set, _ := fm.GetFlag("flag_familyname_set")
	flag_yob_set, _ := fm.GetFlag("flag_yob_set")
	flag_gender_set, _ := fm.GetFlag("flag_gender_set")
	flag_location_set, _ := fm.GetFlag("flag_location_set")
	flag_offerings_set, _ := fm.GetFlag("flag_offerings_set")
	flag_back_set, _ := fm.GetFlag("flag_back_set")

	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		st:            state.NewState(16),
	}

	tests := []struct {
		name     string
		execPath string
		dbKey    storedb.DataTyp
		value    string
		expected resource.Result
	}{
		{
			name:     "Test fetching first name",
			execPath: "edit_first_name",
			dbKey:    storedb.DATA_FIRST_NAME,
			value:    "John",
			expected: resource.Result{
				FlagReset: []uint32{flag_back_set},
				FlagSet:   []uint32{flag_firstname_set},
				Content:   "John",
			},
		},
		{
			name:     "Test fetching family name",
			execPath: "edit_family_name",
			dbKey:    storedb.DATA_FAMILY_NAME,
			value:    "Doe",
			expected: resource.Result{
				FlagReset: []uint32{flag_back_set},
				FlagSet:   []uint32{flag_familyname_set},
				Content:   "Doe",
			},
		},
		{
			name:     "Test fetching year of birth",
			execPath: "edit_yob",
			dbKey:    storedb.DATA_YOB,
			value:    "1980",
			expected: resource.Result{
				FlagReset: []uint32{flag_back_set},
				FlagSet:   []uint32{flag_yob_set},
				Content:   "1980",
			},
		},
		{
			name:     "Test fetching gender",
			execPath: "edit_gender",
			dbKey:    storedb.DATA_GENDER,
			value:    "Male",
			expected: resource.Result{
				FlagReset: []uint32{flag_back_set},
				FlagSet:   []uint32{flag_gender_set},
				Content:   "Male",
			},
		},
		{
			name:     "Test fetching location",
			execPath: "edit_location",
			dbKey:    storedb.DATA_LOCATION,
			value:    "Nairobi",
			expected: resource.Result{
				FlagReset: []uint32{flag_back_set},
				FlagSet:   []uint32{flag_location_set},
				Content:   "Nairobi",
			},
		},
		{
			name:     "Test fetching offerings",
			execPath: "edit_offerings",
			dbKey:    storedb.DATA_OFFERINGS,
			value:    "Fruits",
			expected: resource.Result{
				FlagReset: []uint32{flag_back_set},
				FlagSet:   []uint32{flag_offerings_set},
				Content:   "Fruits",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx = context.WithValue(ctx, "SessionId", sessionId)
			ctx = context.WithValue(ctx, "Language", lang.Language{
				Code: "eng",
			})
			// Set ExecPath to include tt.execPath
			h.st.ExecPath = []string{tt.execPath}

			if tt.value != "" {
				err := store.WriteEntry(ctx, sessionId, tt.dbKey, []byte(tt.value))
				if err != nil {
					t.Fatal(err)
				}
			}

			res, err := h.GetCurrentProfileInfo(ctx, tt.execPath, []byte(""))
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, tt.expected, res, "Result should match the expected output")
		})
	}
}
