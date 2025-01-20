// +build testfsdb

package testutil

import (
	"os"

)

func init() {
	d, err := os.MkdirTemp("", "sarafu-vise-menutraversal-state-")
	if err != nil {
		panic(err)
	}
	override.StateConn = &d
	d, err = os.MkdirTemp("", "sarafu-vise-menutraversal-user-")
	if err != nil {
		panic(err)
	}
	override.UserConn = &d
}
