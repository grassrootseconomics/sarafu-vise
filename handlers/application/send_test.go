package application

import (
	"context"
	"fmt"
	"log"
	"testing"

	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-api/models"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/mocks"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
)

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
		name              string
		input             []byte
		expectError       bool
		expectedRecipient []byte
		expectedResult    resource.Result
	}{
		{
			name:        "Test with invalid recepient",
			input:       []byte("7?1234"),
			expectError: true,
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_recipient},
				Content: "7?1234",
			},
		},
		{
			name:        "Test with valid unregistered recepient",
			input:       []byte("0712345678"),
			expectError: true,
			expectedResult: resource.Result{
				FlagSet: []uint32{flag_invalid_recipient_with_invite},
				Content: "0712345678",
			},
		},
		{
			name:              "Test with valid registered recepient",
			input:             []byte("0711223344"),
			expectError:       false,
			expectedRecipient: []byte(publicKey),
			expectedResult:    resource.Result{},
		},
		{
			name:              "Test with address",
			input:             []byte("0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9"),
			expectError:       false,
			expectedRecipient: []byte("0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9"),
			expectedResult:    resource.Result{},
		},
		{
			name:              "Test with alias recepient",
			input:             []byte("alias123.sarafu.local"),
			expectError:       false,
			expectedRecipient: []byte("0xd4c288865Ce0985a481Eef3be02443dF5E2e4Ea9"),
			expectedResult:    resource.Result{},
		},
		{
			name:              "Test for checksummed address",
			input:             []byte("0x5523058cdffe5f3c1eadadd5015e55c6e00fb439"),
			expectError:       false,
			expectedRecipient: []byte("0x5523058cdFfe5F3c1EaDADD5015E55C6E00fb439"),
			expectedResult:    resource.Result{},
		},
		{
			name:              "Test with valid registered recepient that has white spaces",
			input:             []byte("0711 22 33 44"),
			expectError:       false,
			expectedRecipient: []byte(publicKey),
			expectedResult:    resource.Result{},
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

			if !tt.expectError {
				storedRecipientAddress, err := store.ReadEntry(ctx, sessionId, storedb.DATA_RECIPIENT)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedRecipient, storedRecipientAddress)
			}

			// Assert that the Result FlagSet has the required flags after language switch
			assert.Equal(t, res, tt.expectedResult, "Result should contain flag(s) that have been reset")
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

			res, err := h.MaxAmount(ctx, "send_max_amount", []byte(""))

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedResult, res)
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
		{
			name:      "Test with valid decimal amount",
			input:     []byte("0.149"),
			activeBal: []byte("5"),
			expectedResult: resource.Result{
				Content: "0.14",
			},
		},
		{
			name:      "Test with valid large decimal amount",
			input:     []byte("1.8599999999"),
			activeBal: []byte("5"),
			expectedResult: resource.Result{
				Content: "1.85",
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
