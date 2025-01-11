package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/ssh"
)

func main() {
	var dbDir string
	var sessionId string
	flag.StringVar(&dbDir, "dbdir", ".state", "database dir to read from")
	flag.StringVar(&sessionId, "i", "", "session id")
	flag.Parse()

	if sessionId == "" {
		fmt.Fprintf(os.Stderr, "empty session id\n")
		os.Exit(1)
	}

	ctx := context.Background()

	sshKeyFile := flag.Arg(0)
	if sshKeyFile == "" {
		fmt.Fprintf(os.Stderr, "missing key file argument\n")
		os.Exit(1)
	}

	store, err := ssh.NewSshKeyStore(ctx, dbDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	err = store.AddFromFile(ctx, sshKeyFile, sessionId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
