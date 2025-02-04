package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/lang"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/resource"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/visedriver/request"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/services"
	at "git.grassecon.net/grassrootseconomics/visedriver-africastalking/africastalking"
)

var (
	logg          = logging.NewVanilla().WithDomain("AfricasTalking").WithContextKey("at-session-id")
	scriptDir     = path.Join("services", "registration")
	build         = "dev"
	menuSeparator = ": "
)

func main() {
	config.LoadConfig()

	override := config.NewOverride()
	var size uint
	var engineDebug bool
	var host string
	var port uint
	var err error
	var gettextDir string
	var langs args.LangVar

	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.StringVar(&override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(&override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(&override.ResourceConn, "resource", "?", "resource data directory")
	flag.StringVar(&override.StateConn, "state", "?", "state store connection string")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&host, "h", config.Host(), "http host")
	flag.UintVar(&port, "p", config.Port(), "http port")
	flag.StringVar(&gettextDir, "gettext", "", "use gettext translations from given directory")
	flag.Var(&langs, "language", "add symbol resolution for language")
	flag.Parse()

	config.Apply(override)
	conns, err := config.GetConns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conn specification error: %v\n", err)
		os.Exit(1)
	}

	logg.Infof("start command", "build", build, "conn", conns, "outputsize", size)

	ctx := context.Background()
	ln, err := lang.LanguageFromCode(config.Language())
	if err != nil {
		fmt.Fprintf(os.Stderr, "default language set error: %v", err)
		os.Exit(1)
	}
	ctx = context.WithValue(ctx, "Language", ln)

	pfp := path.Join(scriptDir, "pp.csv")

	cfg := engine.Config{
		Root:          "root",
		OutputSize:    uint32(size),
		FlagCount:     uint32(128),
		MenuSeparator: menuSeparator,
	}

	if engineDebug {
		cfg.EngineDebug = true
	}

	menuStorageService := storage.NewMenuStorageService(conns)
	rs, err := menuStorageService.GetResource(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "menustorageservice: %v\n", err)
		os.Exit(1)
	}

	userdataStore, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "userdatadb: %v\n", err)
		os.Exit(1)
	}

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		fmt.Fprintf(os.Stderr, "dbresource\n")
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "localhandlerservice: %v\n", err)
		os.Exit(1)
	}
	lhs.SetDataStore(&userdataStore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setdatastore: %v\n", err)
		os.Exit(1)
	}

	accountService := services.New(ctx, menuStorageService)

	hl, err := lhs.GetHandler(accountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "httpaccountservice: %v\n", err)
		os.Exit(1)
	}

	stateStore, err := menuStorageService.GetStateStore(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "getstatestore: %v\n", err)
		os.Exit(1)
	}

	rp := &at.ATRequestParser{}
	bsh := request.NewBaseRequestHandler(cfg, rs, stateStore, userdataStore, rp, hl)
	bsh = bsh.WithEngineFunc(lhs.GetEngine)
	sh := at.NewATRequestHandler(bsh)

	mux := http.NewServeMux()
	mux.Handle(config.ATEndpoint(), sh)

	s := &http.Server{
		Addr:    fmt.Sprintf("%s:%s", host, strconv.Itoa(int(port))),
		Handler: mux,
	}
	shutdownFunc := func() {
		sh.Shutdown(ctx)
	}
	s.RegisterOnShutdown(shutdownFunc)

	cint := make(chan os.Signal)
	cterm := make(chan os.Signal)
	signal.Notify(cint, os.Interrupt, syscall.SIGINT)
	signal.Notify(cterm, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case _ = <-cint:
		case _ = <-cterm:
		}
		s.Shutdown(ctx)
	}()
	err = s.ListenAndServe()
	if err != nil {
		logg.Infof("Server closed with error", "err", err)
	}
}
