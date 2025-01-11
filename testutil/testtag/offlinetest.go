// +build !online

package testtag

import (
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	accountservice "git.grassecon.net/grassrootseconomics/sarafu-api/testutil/testservice"
)

var (
	AccountService remote.AccountService = &accountservice.TestAccountService{}
)
