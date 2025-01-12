package store

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"strconv"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

type TransactionData struct {
	TemporaryValue string
	ActiveSym      string
	Amount         string
	PublicKey      string
	Recipient      string
	ActiveDecimal  string
	ActiveAddress  string
}

func ParseAndScaleAmount(storedAmount, activeDecimal string) (string, error) {
	// Parse token decimal
	tokenDecimal, err := strconv.Atoi(activeDecimal)
	if err != nil {

		return "", err
	}

	// Parse amount
	amount, _, err := big.ParseFloat(storedAmount, 10, 0, big.ToZero)
	if err != nil {
		return "", err
	}

	// Scale the amount
	multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tokenDecimal)), nil))
	finalAmount := new(big.Float).Mul(amount, multiplier)

	// Convert finalAmount to a string
	finalAmountStr := new(big.Int)
	finalAmount.Int(finalAmountStr)

	return finalAmountStr.String(), nil
}

func ReadTransactionData(ctx context.Context, store DataStore, sessionId string) (TransactionData, error) {
	data := TransactionData{}
	fieldToKey := map[string]storedb.DataTyp{
		"TemporaryValue": storedb.DATA_TEMPORARY_VALUE,
		"ActiveSym":      storedb.DATA_ACTIVE_SYM,
		"Amount":         storedb.DATA_AMOUNT,
		"PublicKey":      storedb.DATA_PUBLIC_KEY,
		"Recipient":      storedb.DATA_RECIPIENT,
		"ActiveDecimal":  storedb.DATA_ACTIVE_DECIMAL,
		"ActiveAddress":  storedb.DATA_ACTIVE_ADDRESS,
	}

	v := reflect.ValueOf(&data).Elem()
	for fieldName, key := range fieldToKey {
		field := v.FieldByName(fieldName)
		if !field.IsValid() || !field.CanSet() {
			return data, errors.New("invalid struct field: " + fieldName)
		}

		value, err := readStringEntry(ctx, store, sessionId, key)
		if err != nil {
			return data, err
		}
		field.SetString(value)
	}

	return data, nil
}

func readStringEntry(ctx context.Context, store DataStore, sessionId string, key storedb.DataTyp) (string, error) {
	entry, err := store.ReadEntry(ctx, sessionId, key)
	if err != nil {
		return "", err
	}
	return string(entry), nil
}
