package store

import (
	"context"

	"git.defalsify.org/vise.git/db"
	fsdb "git.defalsify.org/vise.git/db/fs"
	"git.defalsify.org/vise.git/logging"
)

var (
	logg = logging.NewVanilla().WithDomain("adminstore")
)

type AdminStore struct {
	ctx     context.Context
	FsStore db.Db
}

func NewAdminStore(ctx context.Context, fileName string) (*AdminStore, error) {
	fsStore, err := getFsStore(ctx, fileName)
	if err != nil {
		return nil, err
	}
	return &AdminStore{ctx: ctx, FsStore: fsStore}, nil
}

func getFsStore(ctx context.Context, connectStr string) (db.Db, error) {
	fsStore := fsdb.NewFsDb()
	err := fsStore.Connect(ctx, connectStr)
	fsStore.SetPrefix(db.DATATYPE_USERDATA)
	if err != nil {
		return nil, err
	}
	return fsStore, nil
}

// Checks if the given sessionId is listed as an admin.
func (as *AdminStore) IsAdmin(sessionId string) (bool, error) {
	_, err := as.FsStore.Get(as.ctx, []byte(sessionId))
	if err != nil {
		if db.IsNotFound(err) {
			logg.Printf(logging.LVL_INFO, "Returning false because session id was not found")
			return false, nil
		} else {
			return false, err
		}
	}

	return true, nil
}
