package config

import (
	viseconfig "git.grassecon.net/grassrootseconomics/visedriver/config"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

func NewOverride() *viseconfig.Override {
	o := &viseconfig.Override{
		StateConnMode:    storage.DBMODE_TEXT,
		ResourceConnMode: storage.DBMODE_TEXT,
		UserConnMode:     storage.DBMODE_BINARY,
	}
	return o
}

func Apply(o *viseconfig.Override) error {
	viseconfig.ApplyConn(o)
	return nil
}
