package authplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

var ErrStartIndeterminate = errors.New("authentication start already attempted; reconcile provider handoff without redispatch")
var ErrStartConflict = errors.New("authentication start key belongs to another intent")

// StartAttempts retains dispatch intent before an external authentication handoff.
// Markers contain only a digest, never provider codes, URLs, credentials or target
// locators. Existing markers cannot authorize dispatch, including after restart.
// The trusted caller owns the local directory and retention; deleting a marker
// removes replay protection. No automatic expiry or success inference is provided.
type StartAttempts struct{ directory string }

// OpenStartAttempts uses an existing local directory. Local file permissions
// are managed by the OS/installer; they are not a second authorization plane.
func OpenStartAttempts(directory string) (*StartAttempts, error) {
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("authentication starts require a directory")
	}
	return &StartAttempts{directory: directory}, nil
}

// StartAttempt checks trusted admission and retains exact intent before calling
// the effect owner. Any publication failure forbids dispatch; any driver failure
// leaves the marker in place. Reusing the same key never starts another flow.
// This is handoff attribution, not durable provider-session or credential recovery.
func (m *AuthSessionManager) StartAttempt(ctx context.Context, attempts *StartAttempts, key string, in StartInput, authorize func() error) (StartOutput, error) {
	if attempts == nil || authorize == nil {
		return StartOutput{}, errors.New("authentication start requires retained intent and trusted admission")
	}
	normalized, err := normalizeStartInput(in)
	if err != nil {
		return StartOutput{}, err
	}
	if normalized.AuthMode != "browser" && normalized.AuthMode != "device" {
		return StartOutput{}, errors.New("authentication start requires browser or device mode")
	}
	if err := ctx.Err(); err != nil {
		return StartOutput{}, err
	}
	if err := authorize(); err != nil {
		return StartOutput{}, err
	}
	if err := attempts.begin(key, normalized); err != nil {
		return StartOutput{}, err
	}
	// Publication may outlive ingress admission. Withdrawal still forbids effects,
	// while the retained marker conservatively prevents future blind redispatch.
	if err := ctx.Err(); err != nil {
		return StartOutput{}, err
	}
	if err := authorize(); err != nil {
		return StartOutput{}, err
	}
	out, err := m.Start(ctx, normalized)
	if err != nil {
		// Once dispatch begins, driver failure cannot establish no external
		// effect. Preserve typed uncertainty on the first acknowledgement too.
		return StartOutput{}, errors.Join(ErrStartIndeterminate, err)
	}
	return out, nil
}

func (a *StartAttempts) begin(key string, in StartInput) error {
	if _, err := OpenStartAttempts(a.directory); err != nil {
		return err
	}
	if len(key) < 16 || len(key) > 256 {
		return errors.New("authentication start requires a 16–256 byte retry key")
	}
	for _, b := range []byte(key) {
		if b < 0x21 || b > 0x7e {
			return errors.New("authentication start retry key must be visible ASCII")
		}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	expected := fmt.Appendf(nil, "auth-start-v1\n%x\n", digest)
	keyDigest := sha256.Sum256([]byte("local-operator\x00POST\x00/api/v1/auth-sessions\x00" + key))
	path := filepath.Join(a.directory, fmt.Sprintf("%x.attempt", keyDigest))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return inspectStartAttempt(path, expected)
	}
	if err != nil {
		return err
	}
	written, writeErr := file.Write(expected)
	if writeErr == nil && written != len(expected) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	directory, err := os.Open(a.directory)
	if err != nil {
		return err
	}
	return errors.Join(atomicfile.SyncDirectory(directory), directory.Close())
}

func inspectStartAttempt(path string, expected []byte) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return ErrStartIndeterminate
	}
	file, err := os.Open(path)
	if err != nil {
		return ErrStartIndeterminate
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrStartIndeterminate
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil {
		return ErrStartIndeterminate
	}
	if !bytes.Equal(raw, expected) {
		return ErrStartConflict
	}
	return ErrStartIndeterminate
}
