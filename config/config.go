package config

import (
	"git.grassecon.net/grassrootseconomics/visedriver/env"
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
	apiconfig "git.grassecon.net/grassrootseconomics/sarafu-api/config"
)

func init() {
	env.LoadEnvVariables()
}

const (
	defaultSSHHost string = "127.0.0.1"
	defaultSSHPort uint = 7122
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

func Host() string {
	return apiconfig.Host()
}

func Port() uint {
	return apiconfig.Port()
}

func HostSSH() string {
	return defaultSSHHost
}

func PortSSH() uint {
	return defaultSSHPort
}

func ATEndpoint() string {
	return env.GetEnv("AT_ENDPOINT", "/")
}
