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
	httprequest "git.grassecon.net/grassrootseconomics/visedriver/request/http"
	"git.grassecon.net/grassrootseconomics/visedriver/request"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/services"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
)

var (
	logg          = logging.NewVanilla()
	scriptDir     = path.Join("services", "registration")
	menuSeparator = ": "
)

func main() {
	config.LoadConfig()

	var connStr string
	var resourceDir string
	var size uint
	var engineDebug bool
	var host string
	var port uint
	var err error
	var gettextDir string
	var langs args.LangVar

	flag.StringVar(&resourceDir, "resourcedir", path.Join("services", "registration"), "resource dir")
	flag.StringVar(&connStr, "c", "", "connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&host, "h", config.Host(), "http host")
	flag.UintVar(&port, "p", config.Port(), "http port")
	flag.StringVar(&gettextDir, "gettext", "", "use gettext translations from given directory")
	flag.Var(&langs, "language", "add symbol resolution for language")
	flag.Parse()

	if connStr == "" {
		connStr = config.DbConn()
	}
	connData, err := storage.ToConnData(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr err: %v", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", connData, "resourcedir", resourceDir, "outputsize", size)

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

	menuStorageService := storage.NewMenuStorageService(connData, resourceDir)

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
	defer userdataStore.Close()

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

	accountService := services.New(ctx, menuStorageService, connData)
	
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
	defer stateStore.Close()

	//accountService := services.New(ctx, menuStorageService, connData)

	rp := &httprequest.DefaultRequestParser{}
	bsh := request.NewBaseRequestHandler(cfg, rs, stateStore, userdataStore, rp, hl)
	sh := httprequest.NewHTTPRequestHandler(bsh)
	s := &http.Server{
		Addr:    fmt.Sprintf("%s:%s", host, strconv.Itoa(int(port))),
		Handler: sh,
	}
	s.RegisterOnShutdown(sh.Shutdown)

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
