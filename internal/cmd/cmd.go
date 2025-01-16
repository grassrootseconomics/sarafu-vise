package cmd

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/asm"
	"git.defalsify.org/vise.git/logging"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var (
	logg = logging.NewVanilla().WithDomain("cmd").WithContextKey("SessionId")
)

type Cmd struct {
	sessionId string
	conn storage.ConnData
	flagParser *asm.FlagParser
	cmd int
	enable bool
	exec func(ctx context.Context, ss storage.StorageService) error
}

func NewCmd(conn storage.ConnData, sessionId string, flagParser *asm.FlagParser) *Cmd {
	return &Cmd{
		conn: conn,
		sessionId: sessionId,
		flagParser: flagParser,
	}
}

func (c *Cmd) Exec(ctx context.Context, ss storage.StorageService) error {
	return c.exec(ctx, ss)
}

func (c *Cmd) execAdmin(ctx context.Context, ss storage.StorageService) error {
	pe, err := ss.GetPersister(ctx)
	if err != nil {
		return err
	}
	err = pe.Load(c.sessionId)
	if err != nil {
		return err
	}
	defer func() {
		err := pe.Save(c.sessionId)
		if err != nil {
			logg.ErrorCtxf(ctx, "failed persister save: %v", err)
		}
	}()

	st := pe.GetState()
	flag, err := c.flagParser.GetFlag("flag_admin_privilege")
	if err != nil {
		return err
	}
	if c.enable {
		logg.InfoCtxf(ctx, "setting admin flag", "flag", flag)
		st.SetFlag(flag)
	} else {
		st.ResetFlag(flag)
	}
	return nil
}

func (c *Cmd) parseCmdAdmin(cmd string, param string, more []string) (bool, error) {
	if cmd == "admin" {
		if param == "1" {
			c.enable = true
		} else if param != "0" {
			return false, fmt.Errorf("invalid parameter: %v", param)
		}
		c.exec = c.execAdmin
		return true, nil
	}
	return false, nil
}

func (c *Cmd) Parse(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("Wrong number of arguments: %v", args)
	}
	cmd := args[0]
	param := args[1]
	args = args[2:]

	r, err := c.parseCmdAdmin(cmd, param, args)
	if err != nil {
		return err
	}
	if r {
		return nil
	}

	return fmt.Errorf("unknown subcommand: %s", cmd)
}


