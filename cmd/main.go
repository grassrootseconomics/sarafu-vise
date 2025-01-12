package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/lang"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/visedriver/env"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	httpremote "git.grassecon.net/grassrootseconomics/sarafu-api/remote/http"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
)

var (
	logg          = logging.NewVanilla()
	scriptDir     = path.Join("services", "registration")
	menuSeparator = ": "
)

func init() {
	env.LoadEnvVariables()
}

// TODO: external script automatically generate language handler list from select language vise code OR consider dynamic menu generation script possibility
func main() {
	config.LoadConfig()

	var connStr string
	var size uint
	var sessionId string
	var database string
	var engineDebug bool
	var resourceDir string
	var err error
	var gettextDir string
	var langs args.LangVar

	flag.StringVar(&resourceDir, "resourcedir", scriptDir, "resource dir")
	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&connStr, "c", "", "connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&gettextDir, "gettext", "", "use gettext translations from given directory")
	flag.Var(&langs, "language", "add symbol resolution for language")
	flag.Parse()

	if connStr != "" {
		connStr = config.DbConn
	}
	connData, err := storage.ToConnData(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr err: %v", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", connData, "outputsize", size)

	if len(langs.Langs()) == 0 {
		langs.Set(config.DefaultLanguage)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)
	ctx = context.WithValue(ctx, "Database", database)

	ln, err := lang.LanguageFromCode(config.DefaultLanguage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "default language set error: %v", err)
		os.Exit(1)
	}
	ctx = context.WithValue(ctx, "Language", ln)

	pfp := path.Join(scriptDir, "pp.csv")

	cfg := engine.Config{
		Root:          "root",
		SessionId:     sessionId,
		OutputSize:    uint32(size),
		FlagCount:     uint32(128),
		MenuSeparator: menuSeparator,
	}

	menuStorageService := storage.NewMenuStorageService(connData, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "menu storage service error: %v", err)
		os.Exit(1)
	}
	
	if gettextDir != "" {
		menuStorageService = menuStorageService.WithGettext(gettextDir, langs.Langs())
	}

	rs, err := menuStorageService.GetResource(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	pe, err := menuStorageService.GetPersister(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	userdatastore, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	lhs.SetDataStore(&userdatastore)
	lhs.SetPersister(pe)

	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	accountService := &httpremote.HTTPAccountService{}
	hl, err := lhs.GetHandler(accountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	en := lhs.GetEngine()
	en = en.WithFirst(hl.Init)
	if engineDebug {
		en = en.WithDebug(nil)
	}

	err = engine.Loop(ctx, en, os.Stdin, os.Stdout, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loop exited with error: %v\n", err)
		os.Exit(1)
	}
}
