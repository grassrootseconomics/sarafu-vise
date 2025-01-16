package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	"git.defalsify.org/vise.git/logging"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/internal/cmd"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/application"
)

var (
	logg = logging.NewVanilla().WithContextKey("SessionId")
	scriptDir     = path.Join("services", "registration")
)

func main() {
	config.LoadConfig()

	var sessionId string
	var connStr string

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&connStr, "c", "", "connection string")
	flag.Parse()


	if connStr == "" {
		connStr = config.DbConn()
	}
	connData, err := storage.ToConnData(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr err: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	pfp := path.Join(scriptDir, "pp.csv")
	flagParser, err := application.NewFlagManager(pfp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flagparser fail: %v\n", err)
		os.Exit(1)
	}

	x := cmd.NewCmd(connData, sessionId, flagParser)
	err = x.Parse(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd parse fail: %v\n", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", connData, "subcmd", x)

	menuStorageService := storage.NewMenuStorageService(connData, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "menu storage service error: %v\n", err)
		os.Exit(1)
	}

	err = x.Exec(ctx, menuStorageService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd exec error: %v\n", err)
		os.Exit(1)
	}

}
