package store

import (
	"testing"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/alecthomas/assert/v2"
)

func TestTruncateDecimalString(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		decimalPlaces int
		want          string
		expectError   bool
	}{
		{
			name:          "whole number",
			input:         "4",
			decimalPlaces: 2,
			want:          "4.00",
			expectError:   false,
		},
		{
			name:          "single decimal",
			input:         "4.1",
			decimalPlaces: 2,
			want:          "4.10",
			expectError:   false,
		},
		{
			name:          "one decimal place",
			input:         "4.19",
			decimalPlaces: 1,
			want:          "4.1",
			expectError:   false,
		},
		{
			name:          "truncates to 2 dp",
			input:         "0.149",
			decimalPlaces: 2,
			want:          "0.14",
			expectError:   false,
		},
		{
			name:          "does not round",
			input:         "1.8599999999",
			decimalPlaces: 2,
			want:          "1.85",
			expectError:   false,
		},
		{
			name:          "high precision input",
			input:         "123.456789",
			decimalPlaces: 4,
			want:          "123.4567",
			expectError:   false,
		},
		{
			name:          "zero",
			input:         "0",
			decimalPlaces: 2,
			want:          "0.00",
			expectError:   false,
		},
		{
			name:          "invalid input string",
			input:         "abc",
			decimalPlaces: 2,
			want:          "",
			expectError:   true,
		},
		{
			name:          "edge rounding case",
			input:         "4.99999999",
			decimalPlaces: 2,
			want:          "4.99",
			expectError:   false,
		},
		{
			name:          "small value",
			input:         "0.0001",
			decimalPlaces: 2,
			want:          "0.00",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TruncateDecimalString(tt.input, tt.decimalPlaces)

			if tt.expectError {
				if err == nil {
					t.Errorf("TruncateDecimalString(%q, %d) expected error, got nil", tt.input, tt.decimalPlaces)
				}
				return
			}

			if err != nil {
				t.Errorf("TruncateDecimalString(%q, %d) unexpected error: %v", tt.input, tt.decimalPlaces, err)
				return
			}

			if got != tt.want {
				t.Errorf("TruncateDecimalString(%q, %d) = %q, want %q", tt.input, tt.decimalPlaces, got, tt.want)
			}
		})
	}
}

func TestParseAndScaleAmount(t *testing.T) {
	tests := []struct {
		name        string
		amount      string
		decimals    string
		want        string
		expectError bool
	}{
		{
			name:        "whole number",
			amount:      "123",
			decimals:    "2",
			want:        "12300",
			expectError: false,
		},
		{
			name:        "decimal number",
			amount:      "123.45",
			decimals:    "2",
			want:        "12345",
			expectError: false,
		},
		{
			name:        "zero decimals",
			amount:      "123.45",
			decimals:    "0",
			want:        "123",
			expectError: false,
		},
		{
			name:        "large number",
			amount:      "1000000.01",
			decimals:    "6",
			want:        "1000000010000",
			expectError: false,
		},
		{
			name:        "invalid amount",
			amount:      "abc",
			decimals:    "2",
			want:        "",
			expectError: true,
		},
		{
			name:        "invalid decimals",
			amount:      "123.45",
			decimals:    "abc",
			want:        "",
			expectError: true,
		},
		{
			name:        "zero amount",
			amount:      "0",
			decimals:    "2",
			want:        "0",
			expectError: false,
		},
		{
			name:        "high decimals",
			amount:      "1.85",
			decimals:    "18",
			want:        "1850000000000000000",
			expectError: false,
		},
		{
			name:        "6 d.p",
			amount:      "2.32",
			decimals:    "6",
			want:        "2320000",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAndScaleAmount(tt.amount, tt.decimals)

			// Check error cases
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseAndScaleAmount(%q, %q) expected error, got nil", tt.amount, tt.decimals)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseAndScaleAmount(%q, %q) unexpected error: %v", tt.amount, tt.decimals, err)
				return
			}

			if got != tt.want {
				t.Errorf("ParseAndScaleAmount(%q, %q) = %v, want %v", tt.amount, tt.decimals, got, tt.want)
			}
		})
	}
}

func TestReadTransactionData(t *testing.T) {
	sessionId := "session123"
	publicKey := "0X13242618721"
	ctx, store := InitializeTestDb(t)

	// Test transaction data
	transactionData := map[storedb.DataTyp]string{
		storedb.DATA_TEMPORARY_VALUE: "0712345678",
		storedb.DATA_ACTIVE_SYM:      "SRF",
		storedb.DATA_AMOUNT:          "1000000",
		storedb.DATA_PUBLIC_KEY:      publicKey,
		storedb.DATA_RECIPIENT:       "0x41c188d63Qa",
		storedb.DATA_ACTIVE_DECIMAL:  "6",
		storedb.DATA_ACTIVE_ADDRESS:  "0xd4c288865Ce",
	}

	// Store the data
	for key, value := range transactionData {
		if err := store.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	expectedResult := TransactionData{
		TemporaryValue: "0712345678",
		ActiveSym:      "SRF",
		Amount:         "1000000",
		PublicKey:      publicKey,
		Recipient:      "0x41c188d63Qa",
		ActiveDecimal:  "6",
		ActiveAddress:  "0xd4c288865Ce",
	}

	data, err := ReadTransactionData(ctx, store, sessionId)

	assert.NoError(t, err)
	assert.Equal(t, expectedResult, data)
}
