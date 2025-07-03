package application

import (
	"context"
	"testing"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-api/models"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	"github.com/alecthomas/assert/v2"
)

func TestCreateAccount(t *testing.T) {
	sessionId := "session123"
	ctx, userStore := InitializeTestStore(t)
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	_, logdb := InitializeTestLogdbStore(t)

	logDb := store.LogDb{
		Db: logdb,
	}

	fm, err := NewFlagManager(flagsPath)
	if err != nil {
		t.Logf(err.Error())
	}

	flag_account_created, err := fm.GetFlag("flag_account_created")
	flag_account_creation_failed, _ := fm.GetFlag("flag_account_creation_failed")

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
				FlagSet:   []uint32{flag_account_created},
				FlagReset: []uint32{flag_account_creation_failed},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAccountService := new(mocks.MockAccountService)

			h := &MenuHandlers{
				userdataStore:  userStore,
				accountService: mockAccountService,
				logDb:          logDb,
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
