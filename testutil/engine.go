package testutil

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/visedriver/initializers"
	"git.grassecon.net/grassrootseconomics/visedriver/config"
	"git.grassecon.net/grassrootseconomics/visedriver/handlers"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"git.grassecon.net/grassrootseconomics/visedriver/testutil/testservice"
	"git.grassecon.net/grassrootseconomics/visedriver/testutil/testtag"
	testdataloader "github.com/peteole/testdata-loader"
	"git.grassecon.net/grassrootseconomics/visedriver/remote"
)

var (
	logg        = logging.NewVanilla()
	baseDir     = testdataloader.GetBasePath()
	scriptDir   = path.Join(baseDir, "services", "registration")
	setDbType   string
	setConnStr  string
	setDbSchema string
)

func init() {
	initializers.LoadEnvVariablesPath(baseDir)
	config.LoadConfig()
}

// SetDatabase updates the database used by TestEngine
func SetDatabase(database, connStr, dbSchema string) {
	setDbType = database
	setConnStr = connStr
	setDbSchema = dbSchema
}

// CleanDatabase removes all test data from the database
func CleanDatabase() {
	if setDbType == "postgres" {
		ctx := context.Background()
		// Update the connection string with the new search path
		updatedConnStr, err := updateSearchPath(setConnStr, setDbSchema)
		if err != nil {
			log.Fatalf("Failed to update search path: %v", err)
		}

		dbConn, err := pgxpool.New(ctx, updatedConnStr)
		if err != nil {
			log.Fatalf("Failed to connect to database for cleanup: %v", err)
		}
		defer dbConn.Close()

		query := fmt.Sprintf("DELETE FROM %s.kv_vise;", setDbSchema)
		_, execErr := dbConn.Exec(ctx, query)
		if execErr != nil {
			log.Printf("Failed to cleanup table %s.kv_vise: %v", setDbSchema, execErr)
		} else {
			log.Printf("Successfully cleaned up table %s.kv_vise", setDbSchema)
		}
	} else {
		setConnStr, _ := filepath.Abs(setConnStr)
		if err := os.RemoveAll(setConnStr); err != nil {
			log.Fatalf("Failed to delete state store %s: %v", setConnStr, err)
		}
	}
}

// updateSearchPath updates the search_path (schema) to be used in the connection
func updateSearchPath(connStr string, newSearchPath string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", fmt.Errorf("invalid connection string: %w", err)
	}

	// Parse the query parameters
	q := u.Query()

	// Update or add the search_path parameter
	q.Set("search_path", newSearchPath)

	// Rebuild the connection string with updated parameters
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func TestEngine(sessionId string) (engine.Engine, func(), chan bool) {
	var err error
	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	pfp := path.Join(scriptDir, "pp.csv")

	var eventChannel = make(chan bool)

	cfg := engine.Config{
		Root:       "root",
		SessionId:  sessionId,
		OutputSize: uint32(160),
		FlagCount:  uint32(128),
	}

	if setDbType == "postgres" {
		setConnStr = config.DbConn
		setConnStr, err = updateSearchPath(setConnStr, setDbSchema)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
	} else {
		setConnStr, err = filepath.Abs(setConnStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "connstr err: %v", err)
			os.Exit(1)
		}
	}

	conn, err := storage.ToConnData(setConnStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr parse err: %v", err)
		os.Exit(1)
	}

	resourceDir := scriptDir
	menuStorageService := storage.NewMenuStorageService(conn, resourceDir)

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
		testtag.AccountService = &remote.AccountService{}
	}

	switch testtag.AccountService.(type) {
	case *testservice.TestAccountService:
		go func() {
			eventChannel <- false
		}()
	case *remote.AccountService:
		go func() {
			time.Sleep(5 * time.Second) // Wait for 5 seconds
			eventChannel <- true
		}()
	default:
		panic("Unknown account service type")
	}

	hl, err := lhs.GetHandler(testtag.AccountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	en := lhs.GetEngine()
	en = en.WithFirst(hl.Init)
	cleanFn := func() {
		err := en.Finish()
		if err != nil {
			logg.Errorf(err.Error())
		}

		err = menuStorageService.Close()
		if err != nil {
			logg.Errorf(err.Error())
		}
		logg.Infof("testengine storage closed")
	}
	return en, cleanFn, eventChannel
}
