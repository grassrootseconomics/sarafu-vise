package application

import (
	"context"
	"testing"

	"git.defalsify.org/vise.git/lang"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
)

func TestCheckBalance(t *testing.T) {
	ctx, store := InitializeTestStore(t)

	tests := []struct {
		name           string
		sessionId      string
		publicKey      string
		alias          string
		activeSym      string
		activeBal      string
		expectedResult resource.Result
		expectError    bool
	}{
		{
			name:           "User with no active sym",
			sessionId:      "session123",
			publicKey:      "0X98765432109",
			alias:          "",
			activeSym:      "",
			activeBal:      "",
			expectedResult: resource.Result{Content: "Balance: 0.00 \n"},
			expectError:    false,
		},
		{
			name:           "User with active sym",
			sessionId:      "session123",
			publicKey:      "0X98765432109",
			alias:          "",
			activeSym:      "ETH",
			activeBal:      "1.5",
			expectedResult: resource.Result{Content: "Balance: 1.50 ETH\n"},
			expectError:    false,
		},
		{
			name:           "User with active sym and alias",
			sessionId:      "session123",
			publicKey:      "0X98765432109",
			alias:          "user72",
			activeSym:      "SRF",
			activeBal:      "10.967",
			expectedResult: resource.Result{Content: "user72 balance: 10.96 SRF\n"},
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

			if tt.alias != "" {
				err := store.WriteEntry(ctx, tt.sessionId, storedb.DATA_ACCOUNT_ALIAS, []byte(tt.alias))
				if err != nil {
					t.Fatal(err)
				}
			}

			if tt.activeSym != "" {
				err := store.WriteEntry(ctx, tt.sessionId, storedb.DATA_ACTIVE_SYM, []byte(tt.activeSym))
				if err != nil {
					t.Fatal(err)
				}
			}

			if tt.activeBal != "" {
				err := store.WriteEntry(ctx, tt.sessionId, storedb.DATA_ACTIVE_BAL, []byte(tt.activeBal))
				if err != nil {
					t.Fatal(err)
				}
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
