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

	"github.com/grassrootseconomics/go-vise/engine"
	"github.com/grassrootseconomics/go-vise/lang"
	"github.com/grassrootseconomics/go-vise/resource"
	slogging "github.com/grassrootseconomics/go-vise/slog"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/visedriver/request"
	httprequest "git.grassecon.net/grassrootseconomics/visedriver/request/http"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/services"
)

var (
	logg          = slogging.Get().With("component", "HTTP Server")
	scriptDir     = path.Join("services", "registration")
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
	var logDbConnStr string

	flag.StringVar(&override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(&override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(&override.ResourceConn, "resource", "?", "resource data directory")
	flag.StringVar(&override.StateConn, "state", "?", "state store connection string")

	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&host, "h", config.Host(), "http host")
	flag.UintVar(&port, "p", config.Port(), "http port")
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

	ctx := context.Background()

	ln, err := lang.LanguageFromCode(config.Language())
	if err != nil {
		fmt.Fprintf(os.Stderr, "default language set error: %v", err)
		os.Exit(1)
	}
	ctx = context.WithValue(ctx, "Language", ln)

	pfp := path.Join(scriptDir, "pp.csv")

	cfg := engine.Config{
		Root:              "root",
		OutputSize:        uint32(size),
		FlagCount:         uint32(128),
		MenuSeparator:     menuSeparator,
		ResetOnEmptyInput: true,
	}

	if engineDebug {
		cfg.EngineDebug = true
	}

	menuStorageService := storage.NewMenuStorageService(conns)

	rs, err := menuStorageService.GetResource(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	userdataStore, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	lhs.SetDataStore(&userdataStore)

	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	accountService := services.New(ctx, menuStorageService)

	hl, err := lhs.GetHandler(accountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	stateStore, err := menuStorageService.GetStateStore(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	//accountService := services.New(ctx, menuStorageService, connData)

	rp := &httprequest.DefaultRequestParser{}
	bsh := request.NewBaseRequestHandler(cfg, rs, stateStore, userdataStore, rp, hl)
	bsh = bsh.WithEngineFunc(lhs.GetEngine)
	sh := httprequest.NewHTTPRequestHandler(bsh)
	s := &http.Server{
		Addr:    fmt.Sprintf("%s:%s", host, strconv.Itoa(int(port))),
		Handler: sh,
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
