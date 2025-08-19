package application

import (
	"context"
	"log"
	"strconv"
	"testing"

	"git.grassecon.net/grassrootseconomics/common/pin"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
	"github.com/grassrootseconomics/go-vise/resource"
	"github.com/grassrootseconomics/go-vise/state"
)

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

func TestSaveTemporaryPin(t *testing.T) {
	sessionId := "session123"

	ctx, userdatastore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		log.Fatal(err)
	}

	flag_invalid_pin, _ := fm.GetFlag("flag_invalid_pin")

	// Create the MenuHandlers instance with the mock flag manager
	h := &MenuHandlers{
		flagManager:   fm,
		userdataStore: userdatastore,
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
				FlagReset: []uint32{flag_invalid_pin},
			},
		},
		{
			name:  "Invalid Pin entry",
			input: []byte("12343"),
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_pin},
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

func TestConfirmPinChange(t *testing.T) {
	sessionId := "session123"

	mockState := state.NewState(16)
	ctx, store := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, _ := NewFlagManager(flagsPath)
	flag_pin_mismatch, _ := fm.GetFlag("flag_pin_mismatch")
	flag_account_pin_reset, _ := fm.GetFlag("flag_account_pin_reset")

	mockAccountService := new(mocks.MockAccountService)
	h := &MenuHandlers{
		userdataStore:  store,
		flagManager:    fm,
		accountService: mockAccountService,
		st:             mockState,
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
				FlagReset: []uint32{flag_pin_mismatch, flag_account_pin_reset},
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

func TestValidateBlockedNumber(t *testing.T) {
	sessionId := "session123"
	validNumber := "+254712345678"
	invalidNumber := "12343"              // Invalid phone number
	unregisteredNumber := "+254734567890" // Valid but unregistered number
	publicKey := "0X13242618721"
	mockState := state.NewState(128)

	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Fatal(err)
	}
	flag_unregistered_number, _ := fm.GetFlag("flag_unregistered_number")

	h := &MenuHandlers{
		userdataStore: userStore,
		st:            mockState,
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

func TestResetOthersPin(t *testing.T) {
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

	h := &MenuHandlers{
		userdataStore: userStore,
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

	_, err = h.ResetOthersPin(ctx, "reset_others_pin", []byte(""))

	assert.NoError(t, err)
}
