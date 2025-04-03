package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/application"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/internal/cmd"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	logg          = logging.NewVanilla().WithContextKey("SessionId")
	scriptDir     = path.Join("services", "registration")
	menuSeparator = ": "
)

func main() {
	config.LoadConfig()

	override := config.NewOverride()
	var sessionId string
	var size uint
	var engineDebug bool

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(&override.ResourceConn, "resource", "?", "resource data directory")

	flag.StringVar(&override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(&override.StateConn, "state", "?", "state store connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.Parse()

	config.Apply(override)
	conns, err := config.GetConns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conn specification error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pfp := path.Join(scriptDir, "pp.csv")
	flagParser, err := application.NewFlagManager(pfp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flagparser fail: %v\n", err)
		os.Exit(1)
	}

	cfg := engine.Config{
		Root:              "root",
		SessionId:         sessionId,
		OutputSize:        uint32(size),
		FlagCount:         uint32(128),
		MenuSeparator:     menuSeparator,
		EngineDebug:       engineDebug,
		ResetOnEmptyInput: true,
	}

	x := cmd.NewCmd(sessionId, flagParser)
	x = x.WithEngine(cfg)
	err = x.Parse(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd parse fail: %v\n", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", conns, "subcmd", x)

	menuStorageService := storage.NewMenuStorageService(conns)
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
