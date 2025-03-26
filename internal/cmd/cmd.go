package cmd

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/persist"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/application"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
)

var argc map[string]int = map[string]int{
	"reset": 0,
	"admin": 1,
}

var (
	logg = logging.NewVanilla().WithDomain("cmd").WithContextKey("SessionId")
)

type Cmd struct {
	sessionId  string
	conn       storage.ConnData
	flagParser *application.FlagManager
	cmd        int
	enable     bool
	exec       func(ctx context.Context, ss storage.StorageService) error
	engineConfig	*engine.Config
	st	*state.State
}

func NewCmd(sessionId string, flagParser *application.FlagManager) *Cmd {
	return &Cmd{
		sessionId:  sessionId,
		flagParser: flagParser,
	}
}

func (c *Cmd) WithEngine(engineConfig engine.Config) *Cmd {
	c.engineConfig = &engineConfig
	return c
}

func (c *Cmd) Exec(ctx context.Context, ss storage.StorageService) error {
	return c.exec(ctx, ss)
}

func (c *Cmd) engine(ctx context.Context, rs resource.Resource, pe *persist.Persister) (engine.Engine, error) {
	if c.engineConfig == nil {
		return nil, fmt.Errorf("engine config missing")
	}
	en := engine.NewEngine(*c.engineConfig, rs)

	st := pe.GetState()
	if st == nil {
		return nil, fmt.Errorf("persister state fail")
	}
	en = en.WithState(st)
	st.UseDebug()
	ca := pe.GetMemory()
	if ca == nil {
		return nil, fmt.Errorf("persister cache fail")
	}
	en = en.WithMemory(ca)
	logg.DebugCtxf(ctx, "state loaded", "state", st)
	return en, nil
}

func (c *Cmd) execReset(ctx context.Context, ss storage.StorageService) error {
	pe, err := ss.GetPersister(ctx)
	if err != nil {
		return fmt.Errorf("get persister error: %v", err)
	}
	rs, err := ss.GetResource(ctx)
	if err != nil {
		return fmt.Errorf("get resource error: %v", err)
	}
	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		return fmt.Errorf("get dbresource error: %v", err)
	}
	err = pe.Load(c.engineConfig.SessionId)
	if err != nil {
		return fmt.Errorf("persister load error: %v", err)
	}
	en, err := c.engine(ctx, dbResource, pe)
	if err != nil {
		return err
	}
	_, err = en.(*engine.DefaultEngine).Reset(ctx, false)
	if err != nil {
		return err
	}
	st := pe.GetState()
	logg.DebugCtxf(ctx, "state after reset", "state", st)

	err = pe.Save(c.engineConfig.SessionId)
	if err != nil {
		return err
	}
	return nil
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

func (c *Cmd) parseCmdReset(cmd string, param string, more []string) (bool, error) {
	if cmd == "reset" {
		c.enable = false
		c.exec = c.execReset
		return true, nil
	}
	return false, nil
}

func (c *Cmd) Parse(args []string) error {
	var param string
	if len(args) < 1 {
		return fmt.Errorf("Wrong number of arguments: %v", args)
	}
	cmd := args[0]

	n, ok := argc[cmd]
	if !ok {
		return fmt.Errorf("invalid command: %v", cmd)
	}
	if n > 0 {
		if len(args) < n + 1 {
			return fmt.Errorf("Wrong number of arguments, need: %d", n)
		}
		param = args[1]
		args = args[2:]
	}

	r, err := c.parseCmdAdmin(cmd, param, args)
	if err != nil {
		return err
	}
	if r {
		return nil
	}

	r, err = c.parseCmdReset(cmd, param, args)
	if err != nil {
		return err
	}
	if r {
		return nil
	}


	return fmt.Errorf("unknown subcommand: %s", cmd)
}
