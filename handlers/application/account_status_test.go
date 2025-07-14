package application

import (
	"context"
	"testing"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-api/models"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
)

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

func TestCheckBlockedStatus(t *testing.T) {
	ctx, store := InitializeTestStore(t)
	sessionId := "session123"
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}
	flag_account_blocked, _ := fm.GetFlag("flag_account_blocked")
	flag_account_pin_reset, _ := fm.GetFlag("flag_account_pin_reset")

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
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_pin_reset},
			},
		},
		{
			name:                    "Account with 0 wrong PIN attempts",
			currentWrongPinAttempts: "0",
			expectedResult: resource.Result{
				FlagReset: []uint32{flag_account_pin_reset, flag_account_blocked},
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
