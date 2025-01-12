package store

import (
	"context"

	visedb "git.defalsify.org/vise.git/db"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
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
