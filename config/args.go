package config

import (
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
)

type Override struct {
	DbConn *string
	StateConn *string
	ResourceConn *string
	UserConn *string
}

func Apply(o *Override) error {
	viseconfig.ApplyConn(o.DbConn, o.StateConn, o.ResourceConn, o.UserConn)
	return nil
}
