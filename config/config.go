package config

import (
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
	apiconfig "git.grassecon.net/grassrootseconomics/sarafu-api/config"
)

var (
	DbConn = viseconfig.DbConn
	DefaultLanguage = viseconfig.DefaultLanguage
)

func LoadConfig() error {
	err := viseconfig.LoadConfig()
	if err != nil {
		return err
	}
	err = apiconfig.LoadConfig()
	if err != nil {
		return err
	}
	DbConn = viseconfig.DbConn
	DefaultLanguage = viseconfig.DefaultLanguage
	return nil
}
