package db

import (
	"encoding/binary"
	"errors"

	"git.defalsify.org/vise.git/logging"
)

// DataType is a subprefix value used in association with vise/db.DATATYPE_USERDATA.
//
// All keys are used only within the context of a single account. Unless otherwise specified, the user context is the session id.
//
// * The first byte is vise/db.DATATYPE_USERDATA
// * The last 2 bytes are the DataTyp value, big-endian.
// * The intermediate bytes are the id of the user context.
//
// All values are strings
type DataTyp uint16

const (
	// API Tracking id to follow status of account creation
	DATA_TRACKING_ID = iota
	// EVM address returned from API on account creation
	DATA_PUBLIC_KEY
	// Currently active PIN used to authenticate ussd state change requests
	DATA_ACCOUNT_PIN
	// The first name of the user
	DATA_FIRST_NAME
	// The last name of the user
	DATA_FAMILY_NAME
	// The year-of-birth of the user
	DATA_YOB
	// The location of the user
	DATA_LOCATION
	// The gender of the user
	DATA_GENDER
	// The offerings description of the user
	DATA_OFFERINGS
	// The ethereum address of the recipient of an ongoing send request
	DATA_RECIPIENT
	// The voucher value amount of an ongoing send request
	DATA_AMOUNT
	// A general swap field for temporary values
	DATA_TEMPORARY_VALUE
	// Currently active voucher symbol of user
	DATA_ACTIVE_SYM
	// Voucher balance of user's currently active voucher
	DATA_ACTIVE_BAL
	// String boolean indicating whether use of PIN is blocked
	DATA_BLOCKED_NUMBER
	// Reverse mapping of a user's evm address to a session id.
	DATA_PUBLIC_KEY_REVERSE
	// Decimal count of the currently active voucher
	DATA_ACTIVE_DECIMAL
	// EVM address of the currently active voucher
	DATA_ACTIVE_ADDRESS
	//Holds count of the number of incorrect PIN attempts
	DATA_INCORRECT_PIN_ATTEMPTS
	//ISO 639 code for the selected language.
	DATA_SELECTED_LANGUAGE_CODE
)

const (
	// List of valid voucher symbols in the user context.
	DATA_VOUCHER_SYMBOLS DataTyp = 256 + iota
	// List of voucher balances for vouchers valid in the user context.
	DATA_VOUCHER_BALANCES
	// List of voucher decimal counts for vouchers valid in the user context.
	DATA_VOUCHER_DECIMALS
	// List of voucher EVM addresses for vouchers valid in the user context.
	DATA_VOUCHER_ADDRESSES
	// List of senders for valid transactions in the user context.
)

const (
	// List of senders for valid transactions in the user context.
	DATA_TX_SENDERS = 512 + iota
	// List of recipients for valid transactions in the user context.
	DATA_TX_RECIPIENTS
	// List of voucher values for valid transactions in the user context.
	DATA_TX_VALUES
	// List of voucher EVM addresses for valid transactions in the user context.
	DATA_TX_ADDRESSES
	// List of valid transaction hashes in the user context.
	DATA_TX_HASHES
	// List of transaction dates for valid transactions in the user context.
	DATA_TX_DATES
	// List of voucher symbols for valid transactions in the user context.
	DATA_TX_SYMBOLS
	// List of voucher decimal counts for valid transactions in the user context.
	DATA_TX_DECIMALS
)

const (
	// Token transfer list
	DATA_TRANSACTIONS = 1024 + iota
)

var (
	logg = logging.NewVanilla().WithDomain("urdt-common")
)

func typToBytes(typ DataTyp) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(typ))
	return b[:]
}

func PackKey(typ DataTyp, data []byte) []byte {
	v := typToBytes(typ)
	return append(v, data...)
}

func StringToDataTyp(str string) (DataTyp, error) {
	switch str {
	case "DATA_FIRST_NAME":
		return DATA_FIRST_NAME, nil
	case "DATA_FAMILY_NAME":
		return DATA_FAMILY_NAME, nil
	case "DATA_YOB":
		return DATA_YOB, nil
	case "DATA_LOCATION":
		return DATA_LOCATION, nil
	case "DATA_GENDER":
		return DATA_GENDER, nil
	case "DATA_OFFERINGS":
		return DATA_OFFERINGS, nil

	default:
		return 0, errors.New("invalid DataTyp string")
	}
}

// ToBytes converts DataTyp or int to a byte slice
func ToBytes[T ~uint16 | int](value T) []byte {
	bytes := make([]byte, 2)
	binary.BigEndian.PutUint16(bytes, uint16(value))
	return bytes
}
