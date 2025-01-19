package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/ssh"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	wg        sync.WaitGroup
	keyStore  db.Db
	logg      = logging.NewVanilla()
	scriptDir = path.Join("services", "registration")

	build = "dev"
)

func main() {
	config.LoadConfig()

	var override config.Override
	var authConnStr string
	var size uint
	var engineDebug bool
	var stateDebug bool
	var host string
	var port uint
	//flag.StringVar(&authConnStr, "authdb", "", "auth connection string")
	flag.StringVar(&override.DbConn, "c", "?", "default connection string (replaces all unspecified strings)")
	flag.StringVar(&override.ResourceConn, "resource", "?", "resource connection string")
	flag.StringVar(&override.UserConn, "userdata", "?", "userdata store connection string")
	flag.StringVar(&override.StateConn, "state", "?", "state store connection string")
	flag.BoolVar(&engineDebug, "d", false, "use engine debug output")
	flag.UintVar(&size, "s", 160, "max size of output")
	flag.StringVar(&host, "h", config.HostSSH(), "socket host")
	flag.UintVar(&port, "p", config.PortSSH(), "socket port")
	flag.Parse()

	config.Apply(&override)
	conns, err := config.GetConns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "conn specification error: %v\n", err)
		os.Exit(1)
	}

	authConnData, err := storage.ToConnData(authConnStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth connstr err: %v", err)
		os.Exit(1)
	}

	sshKeyFile := flag.Arg(0)
	_, err = os.Stat(sshKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open ssh server private key file: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	logg.WarnCtxf(ctx, "!!!!! WARNING WARNING WARNING")
	logg.WarnCtxf(ctx, "!!!!! =======================")
	logg.WarnCtxf(ctx, "!!!!! This is not a production ready server!")
	logg.WarnCtxf(ctx, "!!!!! Do not expose to internet and only use with tunnel!")
	logg.WarnCtxf(ctx, "!!!!! (See ssh -L <...>)")

	logg.Infof("start command", "conn", conns, "authconn", authConnData, "outputsize", size, "keyfile", sshKeyFile, "host", host, "port", port)

	pfp := path.Join(scriptDir, "pp.csv")

	cfg := engine.Config{
		Root:       "root",
		OutputSize: uint32(size),
		FlagCount:  uint32(128),
	}
	if stateDebug {
		cfg.StateDebug = true
	}
	if engineDebug {
		cfg.EngineDebug = true
	}

	authKeyStore, err := ssh.NewSshKeyStore(ctx, authConnData.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "keystore file open error: %v", err)
		os.Exit(1)
	}
	defer func() {
		logg.TraceCtxf(ctx, "shutdown auth key store reached")
		err = authKeyStore.Close(ctx)
		if err != nil {
			logg.ErrorCtxf(ctx, "keystore close error", "err", err)
		}
	}()

	cint := make(chan os.Signal)
	cterm := make(chan os.Signal)
	signal.Notify(cint, os.Interrupt, syscall.SIGINT)
	signal.Notify(cterm, os.Interrupt, syscall.SIGTERM)

	runner := &ssh.SshRunner{
		Cfg: cfg,
		Debug: engineDebug,
		FlagFile: pfp,
		Conn: conns,
		SrvKeyFile:  sshKeyFile,
		Host:        host,
		Port:        port,
	}
	go func() {
		select {
		case _ = <-cint:
		case _ = <-cterm:
		}
		logg.TraceCtxf(ctx, "shutdown runner reached")
		err := runner.Stop()
		if err != nil {
			logg.ErrorCtxf(ctx, "runner stop error", "err", err)
		}

	}()
	runner.Run(ctx, authKeyStore)
}
