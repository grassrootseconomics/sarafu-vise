package testutil

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"time"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	httpremote "git.grassecon.net/grassrootseconomics/sarafu-api/remote/http"
	"git.grassecon.net/grassrootseconomics/sarafu-api/testutil/testservice"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/testutil/testtag"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CleanDatabase removes all test data from the database
func CleanDatabase() {
	for _, v := range []int8{
		storage.STORETYPE_STATE,
		storage.STORETYPE_USER,
	} {
		conn := conns[v]
		logg.Infof("cleaning test database", "typ", v, "db", conn)
		if conn.DbType() == storage.DBTYPE_POSTGRES {
			ctx := context.Background()
			// Update the connection string with the new search path
			updatedConnStr := conn.Raw()

			dbConn, err := pgxpool.New(ctx, updatedConnStr)
			if err != nil {
				log.Fatalf("Failed to connect to database for cleanup: %v", err)
			}
			defer dbConn.Close()

			setDbSchema := conn.Domain()

			query := fmt.Sprintf("DELETE FROM %s.kv_vise;", setDbSchema)
			_, execErr := dbConn.Exec(ctx, query)
			if execErr != nil {
				log.Printf("Failed to cleanup table %s.kv_vise: %v", setDbSchema, execErr)
			} else {
				log.Printf("Successfully cleaned up table %s.kv_vise", setDbSchema)
			}
		} else if conn.DbType() == storage.DBTYPE_FS || conn.DbType() == storage.DBTYPE_GDBM {
			connStr, _ := filepath.Abs(conn.Path())
			if err := os.RemoveAll(connStr); err != nil {
				log.Fatalf("Failed to delete state store %v: %v", conn, err)
			}
		} else {
			logg.Errorf("store cleanup not handled")
		}
	}
}

func TestEngine(sessionId string) (engine.Engine, func(), chan bool) {
	config.LoadConfig()
	err := config.Apply(override)
	if err != nil {
		panic(fmt.Errorf("args override fail: %v\n", err))
	}
	conns, err = config.GetConns()
	if err != nil {
		panic(fmt.Errorf("get conns fail: %v\n", err))
	}
	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	logg.InfoCtxf(ctx, "loaded engine setup", "conns", conns)
	pfp := path.Join(scriptDir, "pp.csv")

	var eventChannel = make(chan bool)

	cfg := engine.Config{
		Root:       "root",
		SessionId:  sessionId,
		OutputSize: uint32(160),
		FlagCount:  uint32(128),
	}

	menuStorageService := storage.NewMenuStorageService(conns)
	menuStorageService = menuStorageService.WithDb(resourceDb, storage.STORETYPE_RESOURCE)

	rs, err := menuStorageService.GetResource(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resource error: %v", err)
		os.Exit(1)
	}

	pe, err := menuStorageService.GetPersister(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "persister error: %v", err)
		os.Exit(1)
	}

	userDataStore, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "userdb error: %v", err)
		os.Exit(1)
	}

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		fmt.Fprintf(os.Stderr, "dbresource cast error")
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	lhs.SetDataStore(&userDataStore)
	lhs.SetPersister(pe)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	if testtag.AccountService == nil {
		testtag.AccountService = &httpremote.HTTPAccountService{}
	}

	switch testtag.AccountService.(type) {
	case *testservice.TestAccountService:
		go func() {
			eventChannel <- false
		}()
	case remote.AccountService:
		go func() {
			time.Sleep(5 * time.Second) // Wait for 5 seconds
			eventChannel <- true
		}()
	default:
		panic("Unknown account service type")
	}

	// TODO: triggers withfirst assignment
	_, err = lhs.GetHandler(testtag.AccountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	en := lhs.GetEngine(lhs.Cfg, rs, pe)
	cleanFn := func() {
		err := en.Finish(ctx)
		if err != nil {
			logg.Errorf(err.Error())
		}

		err = menuStorageService.Close(ctx)
		if err != nil {
			logg.Errorf(err.Error())
		}
		logg.Infof("testengine storage closed")
	}
	return en, cleanFn, eventChannel
}
