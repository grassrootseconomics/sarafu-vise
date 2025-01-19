package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/debug"
	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/logging"
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

	var connStr string
	var sessionId string
	var database string
	var engineDebug bool
	var err error
	var first bool

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&connStr, "c", "", "connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.Parse()

	if connStr == "" {
		connStr = config.DbConn()
	}
	connData, err := storage.ToConnData(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr err: %v\n", err)
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
