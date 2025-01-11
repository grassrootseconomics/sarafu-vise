package commands

import (
	"context"
	"encoding/json"
	"os"

	"git.defalsify.org/vise.git/logging"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
)

var (
	logg = logging.NewVanilla().WithDomain("adminstore")
)

type Admin struct {
	PhoneNumber string `json:"phonenumber"`
}

type Config struct {
	Admins []Admin `json:"admins"`
}

func Seed(ctx context.Context) error {
	var config Config
	adminstore, err := store.NewAdminStore(ctx, "../admin_numbers")
	store := adminstore.FsStore
	if err != nil {
		return err
	}
	defer store.Close()
	data, err := os.ReadFile("admin_numbers.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	for _, admin := range config.Admins {
		err := store.Put(ctx, []byte(admin.PhoneNumber), []byte("1"))
		if err != nil {
			logg.Printf(logging.LVL_DEBUG, "Failed to insert admin number", admin.PhoneNumber)
			return err
		}
	}
	return nil
}
