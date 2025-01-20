package testutil

import (
	"context"
	"path"

	testdataloader "github.com/peteole/testdata-loader"
	"git.defalsify.org/vise.git/logging"
	fsdb "git.defalsify.org/vise.git/db/fs"
	"git.defalsify.org/vise.git/db"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
)

var (
	logg        = logging.NewVanilla().WithDomain("sarafu-vise.testutil").WithContextKey("SessionId")
	conns storage.Conns
	resourceDb db.Db
	baseDir     = testdataloader.GetBasePath()
	scriptDir   = path.Join(baseDir, "services", "registration")
	override config.Override
)

func init() {
	ctx := context.Background()
	config.EnvPath = baseDir
	resourceDb = fsdb.NewFsDb()
	err := resourceDb.Connect(ctx, scriptDir)
	if err != nil {
		panic(err)
	}
}
