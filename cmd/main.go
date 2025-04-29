package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/lang"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/services"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	logg          = logging.NewVanilla()
	scriptDir     = path.Join("services", "registration")
	menuSeparator = ": "
)

func main() {
	config.LoadConfig()

	override := config.NewOverride()
	var size uint
	var sessionId string
	var engineDebug bool
	var err error
	var gettextDir string
	var langs args.LangVar
	var logDbConnStr string

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(&override.ResourceConn, "resource", "?", "resource data directory")
	flag.StringVar(&override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(&override.StateConn, "state", "?", "state store connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&gettextDir, "gettext", "", "use gettext translations from given directory")
	flag.Var(&langs, "language", "add symbol resolution for language")
	flag.StringVar(&logDbConnStr, "log-c", "db-logs", "log db connection string")
	flag.Parse()

	config.Apply(override)
	conns, err := config.GetConns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conn specification error: %v\n", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", conns, "outputsize", size)

	if len(langs.Langs()) == 0 {
		langs.Set(config.Language())
	}

	ctx := context.Background()

	ln, err := lang.LanguageFromCode(config.Language())
	if err != nil {
		fmt.Fprintf(os.Stderr, "default language set error: %v\n", err)
		os.Exit(1)
	}
	ctx = context.WithValue(ctx, "Language", ln)

	pfp := path.Join(scriptDir, "pp.csv")

	cfg := engine.Config{
		Root:              "root",
		SessionId:         sessionId,
		OutputSize:        uint32(size),
		FlagCount:         uint32(128),
		MenuSeparator:     menuSeparator,
		EngineDebug:       engineDebug,
		ResetOnEmptyInput: true,
	}

	menuStorageService := storage.NewMenuStorageService(conns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "menu storage service error: %v\n", err)
		os.Exit(1)
	}

	if gettextDir != "" {
		menuStorageService = menuStorageService.WithGettext(gettextDir, langs.Langs())
	}

	rs, err := menuStorageService.GetResource(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get resource error: %v\n", err)
		os.Exit(1)
	}

	pe, err := menuStorageService.GetPersister(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get persister error: %v\n", err)
		os.Exit(1)
	}

	userdatastore, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get userdata db error: %v\n", err)
		os.Exit(1)
	}

	logdb, err := menuStorageService.GetLogDb(ctx, userdatastore, logDbConnStr, "user-data")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get log db error: %v\n", err)
		os.Exit(1)
	}

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		fmt.Fprintf(os.Stderr, "get dbresource error: %v\n", err)
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	lhs.SetDataStore(&userdatastore)
	lhs.SetLogDb(&logdb)
	lhs.SetPersister(pe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "localhandler service error: %v\n", err)
		os.Exit(1)
	}

	accountService := services.New(ctx, menuStorageService)
	_, err = lhs.GetHandler(accountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get accounts service handler: %v\n", err)
		os.Exit(1)
	}
	en := lhs.GetEngine(cfg, rs, pe)

	cint := make(chan os.Signal)
	cterm := make(chan os.Signal)
	signal.Notify(cint, os.Interrupt, syscall.SIGINT)
	signal.Notify(cterm, os.Interrupt, syscall.SIGTERM)
	go func() {
		var s os.Signal
		select {
		case s = <-cterm:
		case s = <-cint:
		}
		logg.InfoCtxf(ctx, "stopping on signal", "sig", s)
		en.Finish(ctx)
		os.Exit(0)
	}()

	err = engine.Loop(ctx, en, os.Stdin, os.Stdout, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loop exited with error: %v\n", err)
		os.Exit(1)
	}
}
