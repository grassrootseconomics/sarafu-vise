package config

import (
	apiconfig "git.grassecon.net/grassrootseconomics/sarafu-api/config"
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
	"git.grassecon.net/grassrootseconomics/visedriver/env"
)

var (
	GetConns = viseconfig.GetConns
	EnvPath  string
)

func loadEnv() {
	if EnvPath == "" {
		env.LoadEnvVariables()
	} else {
		env.LoadEnvVariablesPath(EnvPath)
	}
}

const (
	defaultSSHHost  string = "127.0.0.1"
	defaultSSHPort  uint   = 7122
	defaultHTTPHost string = "127.0.0.1"
	defaultHTTPPort uint   = 7123
)

func LoadConfig() error {
	loadEnv()
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
