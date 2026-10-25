package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

// connectRecovery owns an explicitly selected local original-intent file.
// Publication is create-once and flushed before dispatch; it grants no consent.
// Records remain after acknowledgement for explicit historical recovery.
type connectRecovery struct {
	root *os.Root
	name string
}

func openConnectRecovery(path string) (*connectRecovery, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("an absolute Connect recovery path is required")
	}
	directory, name := filepath.Split(path)
	if name == "" {
		return nil, errors.New("Connect recovery filename required")
	}
	// The path is explicitly selected by a trusted local operator. os.Root
	// confines the create-once file operations to that directory; it does not
	// implement another privacy or authorization boundary.
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open Connect recovery directory: %w", err)
	}
	return &connectRecovery{root: root, name: name}, nil
}

func (store *connectRecovery) load() (operatorclient.ClientConfigurationIntent, error) {
	raw, err := store.loadRecord()
	if err != nil {
		return operatorclient.ClientConfigurationIntent{}, err
	}
	return operatorclient.RestoreClientConfigurationIntent(raw)
}

func (store *connectRecovery) loadRecord() ([]byte, error) {
	info, err := store.root.Lstat(store.name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("recovery file is not regular or bounded")
	}
	file, err := store.root.Open(store.name)
	if err != nil {
		return nil, errors.New("cannot open recovery file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("recovery file changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, errors.New("cannot read recovery file")
	}
	return raw, nil
}

func (store *connectRecovery) retain(intent operatorclient.ClientConfigurationIntent) error {
	raw, err := intent.RecoveryRecord()
	if err != nil {
		return err
	}
	if _, err := operatorclient.RestoreClientConfigurationIntent(raw); err != nil {
		return err
	}
	return store.retainRecord(raw)
}

func (store *connectRecovery) retainRecord(raw []byte) error {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return errors.New("bounded original recovery record required")
	}
	if original, err := store.loadRecord(); err == nil {
		if !bytes.Equal(original, raw) {
			return errors.New("original intent must be recovered, not replaced")
		}
		return store.sync()
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot inspect original intent")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errors.New("cannot name Connect recovery stage")
	}
	name := ".connect-" + hex.EncodeToString(nonce[:])
	file, err := store.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot stage Connect recovery")
	}
	defer store.root.Remove(name)
	defer file.Close()
	if written, err := file.Write(raw); err != nil || written != len(raw) {
		return errors.New("cannot write Connect recovery")
	}
	if file.Sync() != nil || file.Close() != nil {
		return errors.New("cannot flush Connect recovery")
	}
	if err := store.root.Link(name, store.name); err != nil {
		return errors.New("cannot publish Connect recovery without replacement")
	}
	return store.sync()
}

func (store *connectRecovery) sync() error {
	directory, err := store.root.Open(".")
	if err != nil {
		return errors.New("cannot open Connect recovery durability boundary")
	}
	defer directory.Close()
	if atomicfile.SyncDirectory(directory) != nil {
		return errors.New("cannot flush Connect recovery directory; no apply sent")
	}
	return nil
}
