//go:build online
// +build online

package services

import (
	"context"

	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	httpremote "git.grassecon.net/grassrootseconomics/sarafu-api/remote/http"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

func New(ctx context.Context, storageService storage.StorageService, conn storage.ConnData) remote.AccountService {
	return &httpremote.HTTPAccountService{}
}
