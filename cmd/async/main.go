package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/lang"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/resource"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/services"
	"git.grassecon.net/grassrootseconomics/visedriver/request"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	logg          = logging.NewVanilla()
	scriptDir     = path.Join("services", "registration")
	menuSeparator = ": "
)

type asyncRequestParser struct {
	sessionId string
	input     []byte
}

func (p *asyncRequestParser) GetSessionId(ctx context.Context, r any) (string, error) {
	return p.sessionId, nil
}

func (p *asyncRequestParser) GetInput(r any) ([]byte, error) {
	return p.input, nil
}

func main() {
	config.LoadConfig()

	override := config.NewOverride()
	var sessionId string
	var size uint
	var engineDebug bool
	var host string
	var port uint
	var err error
	var gettextDir string
	var langs args.LangVar

	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(&override.ResourceConn, "resource", "?", "resource data directory")
	flag.StringVar(&override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(&override.StateConn, "state", "?", "state store connection string")

	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
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

	logg.Infof("start command", "conn", conns, "outputsize", size, "sessionId", sessionId)

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
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

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
	//defer userdataStore.Close(ctx)

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	lhs.SetDataStore(&userdataStore)

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
	//defer stateStore.Close(ctx)

	rp := &asyncRequestParser{
		sessionId: sessionId,
	}
	sh := request.NewBaseRequestHandler(cfg, rs, stateStore, userdataStore, rp, hl)
	cfg.SessionId = sessionId
	rqs := request.RequestSession{
		Ctx:    ctx,
		Writer: os.Stdout,
		Config: cfg,
	}

	cint := make(chan os.Signal)
	cterm := make(chan os.Signal)
	signal.Notify(cint, os.Interrupt, syscall.SIGINT)
	signal.Notify(cterm, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case _ = <-cint:
		case _ = <-cterm:
		}
		sh.Shutdown(ctx)
	}()

	for true {
		rqs, err = sh.Process(rqs)
		if err != nil {
			logg.ErrorCtxf(ctx, "error in process: %v", "err", err)
			fmt.Errorf("error in process: %v", err)
			os.Exit(1)
		}
		rqs, err = sh.Output(rqs)
		if err != nil {
			logg.ErrorCtxf(ctx, "error in output: %v", "err", err)
			fmt.Errorf("error in output: %v", err)
			os.Exit(1)
		}
		rqs, err = sh.Reset(ctx, rqs)
		if err != nil {
			logg.ErrorCtxf(ctx, "error in reset: %v", "err", err)
			fmt.Errorf("error in reset: %v", err)
			os.Exit(1)
		}
		fmt.Println("")
		in := bufio.NewReader(os.Stdin)
		//_, err = fmt.Scanln(&rqs.Input)
		s, err := in.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			logg.ErrorCtxf(ctx, "error in input", "err", err)
			fmt.Errorf("error in input: %v", err)
			os.Exit(1)
		}
		rqs.Input = []byte{}
		s = strings.TrimSpace(s)
		rqs.Input = []byte(s)
	}
}
