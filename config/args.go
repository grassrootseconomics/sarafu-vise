package config

import (
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
)

func NewOverride() *viseconfig.Override {
	var a string
	var b string
	var c string
	var d string
	o := &viseconfig.Override{
		DbConn:       &a,
		StateConn:    &b,
		ResourceConn: &c,
		UserConn:     &d,
	}
	return o
}

func Apply(o *viseconfig.Override) error {
	viseconfig.ApplyConn(o)
	return nil
}
