package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	"git.grassecon.net/grassrootseconomics/visedriver/config"
	"git.grassecon.net/grassrootseconomics/visedriver/initializers"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/debug"
	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/logging"
)

var (
	logg      = logging.NewVanilla()
	scriptDir = path.Join("services", "registration")
)

func init() {
	initializers.LoadEnvVariables()
}


func formatItem(k []byte, v []byte) (string, error) {
	o, err := debug.FromKey(k)
	if err != nil {
		return "", err
	}
	s := fmt.Sprintf("%vValue: %v\n\n", o, string(v))
	return s, nil
}

func main() {
	config.LoadConfig()

	var connStr string
	var sessionId string
	var database string
	var engineDebug bool
	var err error

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&connStr, "c", ".state", "connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.Parse()

	if connStr != "" {
		connStr = config.DbConn
	}
	connData, err := storage.ToConnData(config.DbConn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr err: %v", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", connData)

	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	ctx = context.WithValue(ctx, "Database", database)

	resourceDir := scriptDir
	menuStorageService := storage.NewMenuStorageService(connData, resourceDir)

	store, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get userdata db: %v\n", err.Error())
		os.Exit(1)
	}
	store.SetPrefix(db.DATATYPE_USERDATA)

	d, err := store.Dump(ctx, []byte(sessionId))
	if err != nil {
		fmt.Fprintf(os.Stderr, "store dump fail: %v\n", err.Error())
		os.Exit(1)
	}

	for true {
		k, v := d.Next(ctx)
		if k == nil {
			break
		}
		r, err := formatItem(k, v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "format db item error: %v", err)
			os.Exit(1)
		}
		fmt.Printf(r)
	}

	err = store.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}
}
