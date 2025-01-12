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
	defaultHTTPHost string = "127.0.0.1"
	defaultHTTPPort uint = 7123
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
	return nil
}

func DbConn() string {
	return viseconfig.DbConn
}

func Language() string {
	return viseconfig.DefaultLanguage
}

func Host() string {
	return env.GetEnv("HOST", defaultHTTPHost)
}

func Port() uint {
	return env.GetEnvUint("PORT", defaultHTTPPort)
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
