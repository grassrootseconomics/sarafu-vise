//go:build testfsdb
// +build testfsdb

package testutil

import (
	"os"
)

const (
	testDbCookie = true
)

func init() {
	stateDir, err := os.MkdirTemp("", "sarafu-vise-menutraversal-fs-state-")
	if err != nil {
		panic(err)
	}
	override.StateConn = &stateDir
	userDir, err := os.MkdirTemp("", "sarafu-vise-menutraversal-fs-user-")
	if err != nil {
		panic(err)
	}
	override.UserConn = &userDir
}
