package ssh

import (
	"context"
	"encoding/hex"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"

	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/resource"
	"git.defalsify.org/vise.git/state"

	"git.grassecon.net/grassrootseconomics/visedriver/handlers"
	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	"git.grassecon.net/grassrootseconomics/visedriver/remote"
)

var (
	logg = logging.NewVanilla().WithDomain("ssh")
)

type auther struct {
	Ctx context.Context
	keyStore *SshKeyStore
	auth map[string]string
}

func NewAuther(ctx context.Context, keyStore *SshKeyStore) *auther {
	return &auther{
		Ctx: ctx,
		keyStore: keyStore,
		auth: make(map[string]string),
	}
}

func(a *auther) Check(conn ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
	logg.TraceCtxf(a.Ctx, "looking for publickey", "pubkey", fmt.Sprintf("%x", pubKey))
	va, err := a.keyStore.Get(a.Ctx, pubKey)
	if err != nil {
		return nil, err
	}
	ka := hex.EncodeToString(conn.SessionID())
	a.auth[ka] = va 
	fmt.Fprintf(os.Stderr, "connect: %s -> %s\n", ka, va)
	return nil, nil
}

func(a *auther) FromConn(c *ssh.ServerConn) (string, error) {
	if c == nil {
		return "", errors.New("nil server conn")
	}
	if c.Conn == nil {
		return "", errors.New("nil underlying conn")
	}
	return a.Get(c.Conn.SessionID())
}


func(a *auther) Get(k []byte) (string, error) {
	ka := hex.EncodeToString(k)
	v, ok := a.auth[ka]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

type SshRunner struct {
	Ctx context.Context
	Cfg engine.Config
	FlagFile string
	Conn storage.ConnData
	ResourceDir string
	Debug bool
	SrvKeyFile string
	Host string
	Port uint
	wg sync.WaitGroup
	lst net.Listener
}

func(s *SshRunner) serve(ctx context.Context, sessionId string, ch ssh.NewChannel, en engine.Engine) error {
	if ch == nil {
		return errors.New("nil channel")
	}
	if ch.ChannelType() != "session" {
		ch.Reject(ssh.UnknownChannelType, "that is not the channel you are looking for")
		return errors.New("not a session")
	}
	channel, requests, err := ch.Accept()
	if err != nil {
		panic(err)
	}
	defer channel.Close()
	s.wg.Add(1)
	go func(reqIn <-chan *ssh.Request) {
		defer s.wg.Done()
		for req := range reqIn {
			req.Reply(req.Type == "shell", nil)	
		}
		_ = requests
	}(requests)

	cont, err := en.Exec(ctx, []byte{})
	if err != nil {
		return fmt.Errorf("initial engine exec err: %v", err)
	}

	var input [state.INPUT_LIMIT]byte
	for cont {
		c, err := en.Flush(ctx, channel)
		if err != nil {
			return fmt.Errorf("flush err: %v", err)
		}
		_, err = channel.Write([]byte{0x0a})
		if err != nil {
			return fmt.Errorf("newline err: %v", err)
		}
		c, err = channel.Read(input[:])
		if err != nil {
			return fmt.Errorf("read input fail: %v", err)
		}
		logg.TraceCtxf(ctx, "input read", "c", c, "input", input[:c-1])
		cont, err = en.Exec(ctx, input[:c-1])
		if err != nil {
			return fmt.Errorf("engine exec err: %v", err)
		}
		logg.TraceCtxf(ctx, "exec cont", "cont", cont, "en", en)
		_ = c
	}
	c, err := en.Flush(ctx, channel)
	if err != nil {
		return fmt.Errorf("last flush err: %v", err)
	}
	_ = c
	return nil
}

func(s *SshRunner) Stop() error {
	return s.lst.Close()
}

func(s *SshRunner) GetEngine(sessionId string) (engine.Engine, func(), error) {
	ctx := s.Ctx
	menuStorageService := storage.NewMenuStorageService(s.Conn, s.ResourceDir)

	rs, err := menuStorageService.GetResource(ctx)
	if err != nil {
		return nil, nil, err
	}

	pe, err := menuStorageService.GetPersister(ctx)
	if err != nil {
		return nil, nil, err
	}

	userdatastore, err := menuStorageService.GetUserdataDb(ctx)
	if err != nil {
		return nil, nil, err
	}

	dbResource, ok := rs.(*resource.DbResource)
	if !ok {
		return nil, nil, err
	}

	lhs, err := handlers.NewLocalHandlerService(ctx, s.FlagFile, true, dbResource, s.Cfg, rs)
	lhs.SetDataStore(&userdatastore)
	lhs.SetPersister(pe)
	lhs.Cfg.SessionId = sessionId

	if err != nil {
		return nil, nil, err
	}

	// TODO: clear up why pointer here and by-value other cmds
	accountService := &remote.AccountService{}
	hl, err := lhs.GetHandler(accountService)
	if err != nil {
		return nil, nil, err
	}

	en := lhs.GetEngine()
	en = en.WithFirst(hl.Init)
	if s.Debug {
		en = en.WithDebug(nil)
	}
	// TODO: this is getting very hacky!
	closer := func() {
		err := menuStorageService.Close()
		if err != nil {
			logg.ErrorCtxf(ctx, "menu storage service cleanup fail", "err", err)
		}
	}
	return en, closer, nil
}

// adapted example from crypto/ssh package, NewServerConn doc
func(s *SshRunner) Run(ctx context.Context, keyStore *SshKeyStore) {
	s.Ctx = ctx
	running := true

	// TODO: waitgroup should probably not be global
	defer s.wg.Wait()

	auth := NewAuther(ctx, keyStore)
	cfg := ssh.ServerConfig{
		PublicKeyCallback: auth.Check,
	}

	privateBytes, err := os.ReadFile(s.SrvKeyFile)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to load private key", "err", err)
	}
	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to parse private key", "err", err)
	}
	srvPub := private.PublicKey()
	srvPubStr := base64.StdEncoding.EncodeToString(srvPub.Marshal())
	logg.InfoCtxf(ctx, "have server key", "type", srvPub.Type(), "public", srvPubStr)
	cfg.AddHostKey(private)

	s.lst, err = net.Listen("tcp", fmt.Sprintf("%s:%d", s.Host, s.Port))
	if err != nil {
		panic(err)
	}

	for running {
		conn, err := s.lst.Accept()
		if err != nil {
			logg.ErrorCtxf(ctx, "ssh accept error", "err", err)
			running = false
			continue
		}

		go func(conn net.Conn) {
			defer conn.Close()
			for true {
				srvConn, nC, rC, err := ssh.NewServerConn(conn, &cfg)
				if err != nil {
					logg.InfoCtxf(ctx, "rejected client", "err", err)
					return
				}
				logg.DebugCtxf(ctx, "ssh client connected", "conn", srvConn)

				s.wg.Add(1)
				go func() {
					ssh.DiscardRequests(rC)
					s.wg.Done()
				}()
				
				sessionId, err := auth.FromConn(srvConn)
				if err != nil {
					logg.ErrorCtxf(ctx, "Cannot find authentication")
					return
				}
				en, closer, err := s.GetEngine(sessionId)
				if err != nil {
					logg.ErrorCtxf(ctx, "engine won't start", "err", err)
					return
				}
				defer func() {
					err := en.Finish()
					if err != nil {
						logg.ErrorCtxf(ctx, "engine won't stop", "err", err)
					}
					closer()
				}()
				for ch := range nC {
					err = s.serve(ctx, sessionId, ch, en)
					logg.ErrorCtxf(ctx, "ssh server finish", "err", err)
				}
			}
		}(conn)
	}
}
