package config

import (
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

func NewOverride() *viseconfig.Override {
	var a string
	var b string
	var c string
	var d string
	o := &viseconfig.Override{
		DbConn:       &a,
		StateConn:    &b,
		StateConnMode: storage.DBMODE_TEXT,
		ResourceConn: &c,
		ResourceConnMode: storage.DBMODE_TEXT,
		UserConn:     &d,
		UserConnMode: storage.DBMODE_BINARY,
	}
	return o
}

func Apply(o *viseconfig.Override) error {
	viseconfig.ApplyConn(o)
	return nil
}
