package ssh

import (
	"context"
	"fmt"
	"os"
	"path"

	"golang.org/x/crypto/ssh"

	"git.defalsify.org/vise.git/db"

	"git.grassecon.net/grassrootseconomics/visedriver/storage"
	dbstorage "git.grassecon.net/grassrootseconomics/visedriver/storage/db/gdbm"
)

type SshKeyStore struct {
	store db.Db
}

func NewSshKeyStore(ctx context.Context, dbDir string) (*SshKeyStore, error) {
	keyStore := &SshKeyStore{}
	keyStoreFile := path.Join(dbDir, "ssh_authorized_keys.gdbm")
	keyStore.store = dbstorage.NewThreadGdbmDb()
	err := keyStore.store.Connect(ctx, keyStoreFile)
	if err != nil {
		return nil, err
	}
	return keyStore, nil
}

func(s *SshKeyStore) AddFromFile(ctx context.Context, fp string, sessionId string) error {
	_, err := os.Stat(fp)
	if err != nil {
		return fmt.Errorf("cannot open ssh server public key file: %v\n", err)
	}

	publicBytes, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Errorf("Failed to load public key: %v", err)
	}
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(publicBytes)
	if err != nil {
		return fmt.Errorf("Failed to parse public key: %v", err)
	}
	k := append([]byte{0x01}, pubKey.Marshal()...)
	s.store.SetPrefix(storage.DATATYPE_EXTEND)
	logg.Infof("Added key", "sessionId", sessionId, "public key", string(publicBytes))
	return s.store.Put(ctx, k, []byte(sessionId))
}

func(s *SshKeyStore) Get(ctx context.Context, pubKey ssh.PublicKey) (string, error) {
	s.store.SetLanguage(nil)
	s.store.SetPrefix(storage.DATATYPE_EXTEND)
	k := append([]byte{0x01}, pubKey.Marshal()...)
	v, err := s.store.Get(ctx, k)
	if err != nil {
		return "", err
	}
	return string(v), nil
}

func(s *SshKeyStore) Close() error {
	return s.store.Close()
}
