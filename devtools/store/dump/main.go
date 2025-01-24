package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/logging"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/debug"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	logg      = logging.NewVanilla()
	scriptDir = path.Join("services", "registration")
)

func formatItem(k []byte, v []byte, sessionId string) (string, error) {
	o, err := debug.ToKeyInfo(k, sessionId)
	if err != nil {
		return "", err
	}
	s := fmt.Sprintf("%v\t%v\n", o.Label, string(v))

	return s, nil
}

func main() {
	config.LoadConfig()

	override := config.NewOverride()
	var sessionId string
	var database string
	var engineDebug bool
	var err error
	var first bool
	var resourceDir string

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(override.ResourceConn, "resource", "?", "resource data directory")
	flag.StringVar(&resourceDir, "resource-dir", "", "resource data directory. If set, overrides --resource to create a non-binary fsdb for the given path.")
	flag.StringVar(override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(override.StateConn, "state", "?", "state store connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.Parse()

	if resourceDir != "" {
		*override.ResourceConn = resourceDir
		override.ResourceConnMode = storage.DBMODE_TEXT
	}
	config.Apply(override)
	conns, err := config.GetConns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conn specification error: %v\n", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", conns)

	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	ctx = context.WithValue(ctx, "Database", database)

	menuStorageService := storage.NewMenuStorageService(conns)

	store, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get userdata db: %v\n", err.Error())
		os.Exit(1)
	}
	store.SetSession(sessionId)
	store.SetPrefix(db.DATATYPE_USERDATA)

	d, err := store.Dump(ctx, []byte(""))
	if err != nil {
		fmt.Fprintf(os.Stderr, "store dump fail: %v\n", err.Error())
		os.Exit(1)
	}

	for true {
		k, v := d.Next(ctx)
		if k == nil {
			break
		}
		if !first {
			fmt.Printf("Session ID: %s\n---\n", sessionId)
			first = true
		}
		r, err := formatItem(append([]byte{db.DATATYPE_USERDATA}, k...), v, sessionId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "format db item error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf(r)
	}

	err = store.Close(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}
}
