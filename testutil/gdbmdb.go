//go:build testgdbmdb
// +build testgdbmdb

package testutil

import (
	"net/url"
	"os"
)

const (
	testDbCookie = true
)

func init() {
	stateDir, err := os.MkdirTemp("", "sarafu-vise-menutraversal-gdbm-state-")
	if err != nil {
		panic(err)
	}
	stateDir, err = url.JoinPath("gdbm:///", stateDir)
	if err != nil {
		panic(err)
	}
	override.StateConn = &stateDir
	userDir, err := os.MkdirTemp("", "sarafu-vise-menutraversal-gdbm-user-")
	if err != nil {
		panic(err)
	}
	userDir, err = url.JoinPath("gdbm:///", userDir)
	if err != nil {
		panic(err)
	}
	override.UserConn = &userDir
}
