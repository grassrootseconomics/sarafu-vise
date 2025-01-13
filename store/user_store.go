package store

import (
	"context"

	visedb "git.defalsify.org/vise.git/db"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"git.grassecon.net/grassrootseconomics/common/hex"
	"git.grassecon.net/grassrootseconomics/common/identity"
)

// TODO: Rename interface, "datastore" is redundant naming and too general
type DataStore interface {
	visedb.Db
	ReadEntry(ctx context.Context, sessionId string, typ db.DataTyp) ([]byte, error)
	WriteEntry(ctx context.Context, sessionId string, typ db.DataTyp, value []byte) error
}

type UserDataStore struct {
	visedb.Db
}

// ReadEntry retrieves an entry to the userdata store.
func (store *UserDataStore) ReadEntry(ctx context.Context, sessionId string, typ db.DataTyp) ([]byte, error) {
	store.SetPrefix(visedb.DATATYPE_USERDATA)
	store.SetSession(sessionId)
	k := storedb.ToBytes(typ)
	return store.Get(ctx, k)
}

// WriteEntry adds an entry to the userdata store.
// BUG: this uses sessionId twice
func (store *UserDataStore) WriteEntry(ctx context.Context, sessionId string, typ db.DataTyp, value []byte) error {
	store.SetPrefix(visedb.DATATYPE_USERDATA)
	store.SetSession(sessionId)
	k := storedb.ToBytes(typ)
	return store.Put(ctx, k, value)
}

func StoreToPrefixDb(userStore *UserDataStore, pfx []byte) storedb.PrefixDb {
	return storedb.NewSubPrefixDb(userStore.Db, pfx)
}

// IdentityFromAddress fully populates and Identity object from a given
// checksum address.
//
// It is the caller's responsibility to ensure that a valid checksum address
// is passed.
func IdentityFromAddress(ctx context.Context, userStore *UserDataStore, address string) (identity.Identity, error) {
	var err error
	var ident identity.Identity

	ident.ChecksumAddress = address
	ident.NormalAddress, err = hex.NormalizeHex(ident.ChecksumAddress)
	if err != nil {
		return ident, err
	}
	ident.SessionId, err = getSessionIdByAddress(ctx, userStore, ident.NormalAddress)
	if err != nil {
		return ident, err
	}
	return ident, nil
}

// load matching session from address from db store.
func getSessionIdByAddress(ctx context.Context, userStore *UserDataStore, address string) (string, error) {
	// TODO: replace with userdatastore when double sessionid issue fixed
	//r, err := store.ReadEntry(ctx, address, common.DATA_PUBLIC_KEY_REVERSE)
	userStore.Db.SetPrefix(visedb.DATATYPE_USERDATA)
	userStore.Db.SetSession(address)
	r, err := userStore.Db.Get(ctx, storedb.PackKey(storedb.DATA_PUBLIC_KEY_REVERSE, []byte{}))
	if err != nil {
		return "", err
	}
	return string(r), nil
}
