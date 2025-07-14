package application

import (
	"context"
	"log"
	"testing"

	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
)

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
