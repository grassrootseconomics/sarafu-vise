package testutil

import (
	"context"
	"path"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"github.com/grassrootseconomics/go-vise/db"
	fsdb "github.com/grassrootseconomics/go-vise/db/fs"
	"github.com/grassrootseconomics/go-vise/logging"
	testdataloader "github.com/peteole/testdata-loader"
)

var (
	logg       = logging.NewVanilla().WithDomain("sarafu-vise.testutil").WithContextKey("SessionId")
	conns      storage.Conns
	resourceDb db.Db
	baseDir    = testdataloader.GetBasePath()
	scriptDir  = path.Join(baseDir, "services", "registration")
	override   = config.NewOverride()
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
