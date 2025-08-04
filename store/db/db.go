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
	//ISO 639 code for the language initially selected.
	DATA_INITIAL_LANGUAGE_CODE
	//Fully qualified account alias string
	DATA_ACCOUNT_ALIAS
	//currently suggested alias by the api awaiting user's confirmation as accepted account alias
	DATA_SUGGESTED_ALIAS
	//Key used to store a value of 1 for a user to reset their own PIN once they access the menu. 
	DATA_SELF_PIN_RESET
	// Holds the active pool contract address for the swap
	DATA_ACTIVE_POOL_ADDRESS
	// Currently active swap from symbol for the swap
	DATA_ACTIVE_SWAP_FROM_SYM
	// Currently active swap from decimal count for the swap
	DATA_ACTIVE_SWAP_FROM_DECIMAL
	// Holds the active swap from contract address for the swap
	DATA_ACTIVE_SWAP_FROM_ADDRESS
	// Currently active swap from to for the swap
	DATA_ACTIVE_SWAP_TO_SYM
	// Currently active swap to decimal count for the swap
	DATA_ACTIVE_SWAP_TO_DECIMAL
	// Holds the active pool contract address for the swap
	DATA_ACTIVE_SWAP_TO_ADDRESS
	// Holds the max swap amount for the swap
	DATA_ACTIVE_SWAP_MAX_AMOUNT
	// Holds the active swap amount for the swap
	DATA_ACTIVE_SWAP_AMOUNT
	// Holds the active pool name for the swap
	DATA_ACTIVE_POOL_NAME
	// Holds the active pool symbol for the swap
	DATA_ACTIVE_POOL_SYM
	// Holds the send transaction type
	DATA_SEND_TRANSACTION_TYPE
	// Holds the recipient active token (RAT)
	DATA_RECIPIENT_ACTIVE_TOKEN
	// Holds the recipient formatted phone number
	DATA_RECIPIENT_PHONE_NUMBER
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

const (
	// List of voucher symbols in the top pools context.
	DATA_POOL_NAMES = 2048 + iota
	// List of symbols in the top pools context.
	DATA_POOL_SYMBOLS
	// List of contact addresses in the top pools context
	DATA_POOL_ADDRESSES
	// List of swap from voucher symbols in the user context.
	DATA_POOL_FROM_SYMBOLS
	// List of swap from balances for vouchers valid in the pools context.
	DATA_POOL_FROM_BALANCES
	// List of swap from decimal counts for vouchers valid in the pools context.
	DATA_POOL_FROM_DECIMALS
	// List of swap from EVM addresses for vouchers valid in the pools context.
	DATA_POOL_FROM_ADDRESSES
	// List of swap to voucher symbols in the user context.
	DATA_POOL_TO_SYMBOLS
	// List of swap to balances for vouchers valid in the pools context.
	DATA_POOL_TO_BALANCES
	// List of swap to decimal counts for vouchers valid in the pools context.
	DATA_POOL_TO_DECIMALS
	// List of swap to EVM addresses for vouchers valid in the pools context.
	DATA_POOL_TO_ADDRESSES
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
	case "DATA_ACCOUNT_ALIAS":
		return DATA_ACCOUNT_ALIAS, nil
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
