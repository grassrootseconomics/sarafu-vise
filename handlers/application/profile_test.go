package application

import (
	"context"
	"fmt"
	"log"
	"testing"

	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/profile"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
	"github.com/grassrootseconomics/go-vise/lang"
	"github.com/grassrootseconomics/go-vise/resource"
	"github.com/grassrootseconomics/go-vise/state"
	"github.com/stretchr/testify/require"
)

func TestSaveFirstname(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
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

	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(firstName)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_firstname_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: userStore,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveFirstname(ctx, "save_firstname", []byte(firstName))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_FIRST_NAME entry has been updated with the temporary value
	storedFirstName, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME)
	assert.Equal(t, firstName, string(storedFirstName))
}

func TestSaveFamilyname(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
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

	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(familyName)); err != nil {
		t.Fatal(err)
	}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: userStore,
		st:            mockState,
		flagManager:   fm,
	}

	// Call the method
	res, err := h.SaveFamilyname(ctx, "save_familyname", []byte(familyName))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_FAMILY_NAME entry has been updated with the temporary value
	storedFamilyName, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME)
	assert.Equal(t, familyName, string(storedFamilyName))
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

func TestSaveYob(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
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

	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(yob)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_yob_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: userStore,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveYob(ctx, "save_yob", []byte(yob))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_YOB entry has been updated with the temporary value
	storedYob, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_YOB)
	assert.Equal(t, yob, string(storedYob))
}

func TestSaveLocation(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
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

	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(location)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_location_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: userStore,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveLocation(ctx, "save_location", []byte(location))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_LOCATION entry has been updated with the temporary value
	storedLocation, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_LOCATION)
	assert.Equal(t, location, string(storedLocation))
}

func TestSaveGender(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
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
			if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(tt.expectedGender)); err != nil {
				t.Fatal(err)
			}

			mockState.ExecPath = append(mockState.ExecPath, tt.executingSymbol)
			// Create the MenuHandlers instance with the mock store
			h := &MenuHandlers{
				userdataStore: userStore,
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
			storedGender, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_GENDER)
			assert.Equal(t, tt.expectedGender, string(storedGender))
		})
	}
}

func TestSaveOfferings(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
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

	if err := userStore.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(offerings)); err != nil {
		t.Fatal(err)
	}

	expectedResult.FlagSet = []uint32{flag_offerings_set}

	// Create the MenuHandlers instance with the mock store
	h := &MenuHandlers{
		userdataStore: userStore,
		flagManager:   fm,
		st:            mockState,
	}

	// Call the method
	res, err := h.SaveOfferings(ctx, "save_offerings", []byte(offerings))

	// Assert results
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, res)

	// Verify that the DATA_OFFERINGS entry has been updated with the temporary value
	storedOfferings, _ := userStore.ReadEntry(ctx, sessionId, storedb.DATA_OFFERINGS)
	assert.Equal(t, offerings, string(storedOfferings))
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

func TestGetProfileInfo(t *testing.T) {
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

func TestInsertProfileItems(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	mockState := state.NewState(128)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}

	profileDataKeys := []storedb.DataTyp{
		storedb.DATA_FIRST_NAME,
		storedb.DATA_FAMILY_NAME,
		storedb.DATA_GENDER,
		storedb.DATA_YOB,
		storedb.DATA_LOCATION,
		storedb.DATA_OFFERINGS,
	}

	profileItems := []string{"John", "Doe", "Male", "1990", "Nairobi", "Software"}

	h := &MenuHandlers{
		userdataStore: store,
		flagManager:   fm,
		st:            mockState,
		profile: &profile.Profile{
			ProfileItems: profileItems,
			Max:          6,
		},
	}

	res := &resource.Result{}
	err = h.insertProfileItems(ctx, sessionId, res)
	require.NoError(t, err)

	// Loop through profileDataKeys to validate stored values
	for i, key := range profileDataKeys {
		storedValue, err := store.ReadEntry(ctx, sessionId, key)
		require.NoError(t, err)
		assert.Equal(t, profileItems[i], string(storedValue))
	}
}

func TestUpdateAllProfileItems(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	publicKey := "0X13242618721"

	ctx = context.WithValue(ctx, "SessionId", sessionId)

	mockState := state.NewState(128)
	mockAccountService := new(mocks.MockAccountService)

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

	profileDataKeys := []storedb.DataTyp{
		storedb.DATA_FIRST_NAME,
		storedb.DATA_FAMILY_NAME,
		storedb.DATA_GENDER,
		storedb.DATA_YOB,
		storedb.DATA_LOCATION,
		storedb.DATA_OFFERINGS,
	}

	profileItems := []string{"John", "Doe", "Male", "1990", "Nairobi", "Software"}

	expectedResult := resource.Result{
		FlagSet: []uint32{
			flag_firstname_set,
			flag_familyname_set,
			flag_yob_set,
			flag_gender_set,
			flag_location_set,
			flag_offerings_set,
		},
	}

	h := &MenuHandlers{
		userdataStore:  store,
		flagManager:    fm,
		st:             mockState,
		accountService: mockAccountService,
		profile: &profile.Profile{
			ProfileItems: profileItems,
			Max:          6,
		},
	}

	err = store.WriteEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY, []byte(publicKey))
	require.NoError(t, err)

	// Call the function under test
	res, err := h.UpdateAllProfileItems(ctx, "symbol", nil)
	assert.NoError(t, err)

	// Loop through profileDataKeys to validate stored values
	for i, key := range profileDataKeys {
		storedValue, err := store.ReadEntry(ctx, sessionId, key)
		require.NoError(t, err)
		assert.Equal(t, profileItems[i], string(storedValue))
	}

	assert.Equal(t, expectedResult, res)
}
