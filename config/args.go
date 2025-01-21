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

func NewOverride() *Override {
	var a string
	var b string
	var c string
	var d string
	o := &Override{
		DbConn: &a,
		StateConn: &b,
		ResourceConn: &c,
		UserConn: &d,
	}
	return o
}

func Apply(o *Override) error {
	viseconfig.ApplyConn(o.DbConn, o.StateConn, o.ResourceConn, o.UserConn)
	return nil
}
