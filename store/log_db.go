package store

import (
	"context"

	visedb "git.defalsify.org/vise.git/db"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

type LogDb struct {
	visedb.Db
}

func (db *LogDb) WriteLogEntry(ctx context.Context, sessionId string, typ db.DataTyp, v []byte) error {
	db.SetPrefix(visedb.DATATYPE_USERDATA)
	db.SetSession(sessionId)
	k := storedb.ToBytes(typ)
	return db.Put(ctx, k, v)
}

func (db *LogDb) ReadLogEntry(ctx context.Context, sessionId string, typ db.DataTyp) ([]byte, error) {
	db.SetPrefix(visedb.DATATYPE_USERDATA)
	db.SetSession(sessionId)
	k := storedb.ToBytes(typ)
	return db.Get(ctx, k)
}
