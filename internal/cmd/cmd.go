package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/persist"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/application"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"

	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

var argc map[string]int = map[string]int{
	"reset":     0,
	"admin":     1,
	"clone":     1,
	"overwrite": 2,
}

var (
	logg             = logging.NewVanilla().WithDomain("cmd").WithContextKey("SessionId")
	cloneTargetRegex = `^\+000`
)

type Cmd struct {
	sessionId    string
	conn         storage.ConnData
	flagParser   *application.FlagManager
	cmd          int
	enable       bool
	param        string
	exec         func(ctx context.Context, ss storage.StorageService) error
	engineConfig *engine.Config
	st           *state.State
	key          string
	value        string
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

func (c *Cmd) execClone(ctx context.Context, ss storage.StorageService) error {
	re := regexp.MustCompile(cloneTargetRegex)
	if !re.MatchString(c.param) {
		return fmt.Errorf("Clone sessionId must match target: %s", c.param)
	}

	pe, err := ss.GetPersister(ctx)
	if err != nil {
		return fmt.Errorf("get persister error: %v", err)
	}
	err = pe.Load(c.engineConfig.SessionId)
	if err != nil {
		return fmt.Errorf("persister load error: %v", err)
	}

	/// TODO consider DRY with devtools/store/dump
	store, err := ss.GetUserdataDb(ctx)
	if err != nil {
		return fmt.Errorf("store retrieve error: %v", err)
	}

	store.SetSession(c.engineConfig.SessionId)
	store.SetPrefix(db.DATATYPE_USERDATA)
	dmp, err := store.Dump(ctx, []byte(""))
	if err != nil {
		return fmt.Errorf("store dump fail: %v\n", err.Error())
	}

	for true {
		store.SetSession(c.engineConfig.SessionId)
		k, v := dmp.Next(ctx)
		if k == nil {
			break
		}
		store.SetSession(c.param)
		err = store.Put(ctx, k, v)
		if err != nil {
			return fmt.Errorf("user data store clone failed on key: %x", k)
		}
	}

	return pe.Save(c.param)
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

func (c *Cmd) execOverwrite(ctx context.Context, ss storage.StorageService) error {
	store, err := ss.GetUserdataDb(ctx)
	if err != nil {
		return fmt.Errorf("failed to get userdata store: %v", err)
	}

	// Map of symbolic keys to their DataTyp constants
	symbolicKeys := map[string]storedb.DataTyp{
		"first_name":  storedb.DATA_FIRST_NAME,
		"family_name": storedb.DATA_FAMILY_NAME,
		"yob":         storedb.DATA_YOB,
		"location":    storedb.DATA_LOCATION,
		"gender":      storedb.DATA_GENDER,
		"offerings":   storedb.DATA_OFFERINGS,
	}

	// Lookup symbolic key
	dtype, ok := symbolicKeys[strings.ToLower(c.key)]
	if !ok {
		return fmt.Errorf("unknown key '%s'. Available keys: %v", c.key, keysOf(symbolicKeys))
	}

	k := storedb.ToBytes(dtype)

	store.SetPrefix(db.DATATYPE_USERDATA)
	store.SetSession(c.sessionId)

	err = store.Put(ctx, k, []byte(c.value))
	if err != nil {
		return fmt.Errorf("failed to overwrite entry for key %s: %v", c.key, err)
	}

	logg.InfoCtxf(ctx, "overwrote data", "sessionId", c.sessionId, "key", c.key, "value", c.value)
	return nil
}

// keysOf returns a list of keys from the symbolic map for error messages
func keysOf(m map[string]storedb.DataTyp) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

func (c *Cmd) parseCmdClone(cmd string, param string, more []string) (bool, error) {
	if cmd == "clone" {
		c.enable = false
		c.param = param
		c.exec = c.execClone
		return true, nil
	}
	return false, nil
}

func (c *Cmd) parseCmdOverwrite(cmd string, param string, more []string) (bool, error) {
	if cmd == "overwrite" {
		if len(more) < 1 {
			return false, fmt.Errorf("overwrite requires key and value")
		}
		c.key = param
		c.value = more[0]
		c.exec = c.execOverwrite
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
		if len(args) < n+1 {
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

	r, err = c.parseCmdClone(cmd, param, args)
	if err != nil {
		return err
	}
	if r {
		return nil
	}

	r, err = c.parseCmdOverwrite(cmd, param, args)
	if err != nil {
		return err
	}
	if r {
		return nil
	}

	return fmt.Errorf("unknown subcommand: %s", cmd)
}
