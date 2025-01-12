package config

import (
	"strings"

	"git.grassecon.net/grassrootseconomics/common/env"
	apiconfig "git.grassecon.net/grassrootseconomics/sarafu-api/config"
)


var (
	defaultLanguage		   = "eng"
	languages []string
)

var (
	DbConn		string
	DefaultLanguage	    string
	Languages	[]string
)

func setLanguage() error {
	defaultLanguage = env.GetEnv("DEFAULT_LANGUAGE", defaultLanguage)
	languages = strings.Split(env.GetEnv("LANGUAGES", defaultLanguage), ",")
	haveDefaultLanguage := false
	for i, v := range(languages) {
		languages[i] = strings.ReplaceAll(v, " ", "")
		if languages[i] == defaultLanguage {
			haveDefaultLanguage = true
		}
	}

	if !haveDefaultLanguage {
		languages = append([]string{defaultLanguage}, languages...)
	}

	return nil
}



func setConn() error {
	DbConn = env.GetEnv("DB_CONN", "")
	return nil
}

// LoadConfig initializes the configuration values after environment variables are loaded.
func LoadConfig() error {
	//err := apiconfig.SetBase()
	err := apiconfig.LoadConfig()
	if err != nil {
		return err
	}
	err = setConn()
	if err != nil {
		return err
	}
	err = setLanguage()
	if err != nil {
		return err
	}
	DefaultLanguage = defaultLanguage
	Languages = languages

	return nil
}
