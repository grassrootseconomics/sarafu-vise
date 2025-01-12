// +build debugdb

package debug

import (
	"git.defalsify.org/vise.git/db"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

func init() {
	DebugCap |= 1
	dbTypStr[db.DATATYPE_STATE] = "internal state"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TRACKING_ID] = "tracking id"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_PUBLIC_KEY] = "public key"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_ACCOUNT_PIN] = "account pin"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_FIRST_NAME] = "first name"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_FAMILY_NAME] = "family name"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_YOB] = "year of birth"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_LOCATION] = "location"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_GENDER] = "gender"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_OFFERINGS] = "offerings"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_RECIPIENT] = "recipient"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_AMOUNT] = "amount"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TEMPORARY_VALUE] = "temporary value"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_ACTIVE_SYM] = "active sym"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_ACTIVE_BAL] = "active bal"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_BLOCKED_NUMBER] = "blocked number"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_PUBLIC_KEY_REVERSE] = "public_key_reverse"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_ACTIVE_DECIMAL] = "active decimal"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_ACTIVE_ADDRESS] = "active address"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_VOUCHER_SYMBOLS] = "voucher symbols"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_VOUCHER_BALANCES] = "voucher balances"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_VOUCHER_DECIMALS] = "voucher decimals"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_VOUCHER_ADDRESSES] = "voucher addresses"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_SENDERS] = "tx senders"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_RECIPIENTS] = "tx recipients"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_VALUES] = "tx values"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_ADDRESSES] = "tx addresses"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_HASHES] = "tx hashes"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_DATES] = "tx dates"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_SYMBOLS] = "tx symbols"
	dbTypStr[db.DATATYPE_USERDATA + 1 + storedb.DATA_TX_DECIMALS] = "tx decimals"
}
