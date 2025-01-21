// +build testfsdb

package testutil

import (
	"os"

)

func init() {
	stateDir, err := os.MkdirTemp("", "sarafu-vise-menutraversal-state-")
	if err != nil {
		panic(err)
	}
	override.StateConn = &stateDir
	userDir, err := os.MkdirTemp("", "sarafu-vise-menutraversal-user-")
	if err != nil {
		panic(err)
	}
	override.UserConn = &userDir
}
