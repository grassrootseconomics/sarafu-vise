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
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	httpremote "git.grassecon.net/grassrootseconomics/sarafu-api/remote/http"
	devremote "git.grassecon.net/grassrootseconomics/sarafu-api/dev"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/args"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/event"
)

var (
	logg          = logging.NewVanilla()
	scriptDir     = path.Join("services", "registration")
	menuSeparator = ": "
)

type devEmitter struct {
	h *apievent.EventsHandler
}

func (d *devEmitter) emit(ctx context.Context, msg apievent.Msg) error {
	var err error
	if msg.Typ == apievent.EventTokenTransferTag {
		tx, ok := msg.Item.(devremote.Tx)
		if !ok {
			return fmt.Errorf("not a valid tx")
		}
		logg.InfoCtxf(ctx, "tx emit", "tx", tx)
		ev := tx.ToTransferEvent()
		err = d.h.Handle(ctx, apievent.EventTokenTransferTag, &ev)
	} else if msg.Typ == apievent.EventRegistrationTag {
		acc, ok := msg.Item.(devremote.Account)
		if !ok {
			return fmt.Errorf("not a valid tx")
		}
		logg.InfoCtxf(ctx, "account emit", "account", acc)
		ev := acc.ToRegistrationEvent()
		err = d.h.Handle(ctx, apievent.EventRegistrationTag, &ev)
	}
	return err
}

func main() {
	config.LoadConfig()

	var accountService remote.AccountService
	var fakeDir string
	var connStr string
	var size uint
	var sessionId string
	var engineDebug bool
	var resourceDir string
	var err error
	var gettextDir string
	var langs args.LangVar

	flag.StringVar(&resourceDir, "resourcedir", scriptDir, "resource dir")
	flag.StringVar(&sessionId, "session-id", "075xx2123", "session id")
	flag.StringVar(&connStr, "c", "", "connection string")
	flag.StringVar(&fakeDir, "fakedir", "", "if valid path, enables fake api with fsdb backend")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&gettextDir, "gettext", "", "use gettext translations from given directory")
	flag.Var(&langs, "language", "add symbol resolution for language")
	flag.Parse()

	if connStr == "" {
		connStr = config.DbConn()
	}
	connData, err := storage.ToConnData(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connstr err: %v\n", err)
		os.Exit(1)
	}

	logg.Infof("start command", "conn", connData, "outputsize", size)

	if len(langs.Langs()) == 0 {
		langs.Set(config.Language())
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, "SessionId", sessionId)

	ln, err := lang.LanguageFromCode(config.Language())
	if err != nil {
		fmt.Fprintf(os.Stderr, "default language set error: %v\n", err)
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

	menuStorageService := storage.NewMenuStorageService(connData, resourceDir)
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

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		fmt.Fprintf(os.Stderr, "get dbresource error: %v\n", err)
		os.Exit(1)
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, pfp, true, dbResource, cfg, rs)
	lhs.SetDataStore(&userdatastore)
	lhs.SetPersister(pe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "localhandler service error: %v\n", err)
		os.Exit(1)
	}

	if fakeDir != "" {
		svc := devremote.NewDevAccountService()
		svc = svc.WithFs(ctx, fakeDir)
		svc = svc.WithAutoVoucher(ctx, "FOO", 42)
		eu := event.NewEventsUpdater(svc, menuStorageService)
		emitter := &devEmitter{
			h: eu.ToEventsHandler(),
		}
		svc = svc.WithEmitter(emitter.emit)
		svc.AddVoucher(ctx, "BAR")
		accountService = svc
	} else {
		accountService = &httpremote.HTTPAccountService{}
	}
	hl, err := lhs.GetHandler(accountService)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get accounts service handler: %v\n", err)
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
