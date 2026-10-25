package connect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

var ErrAttemptConflict = errors.New("client configuration key belongs to another intent")
var ErrAttemptIndeterminate = errors.New("client configuration attempt exists; reconcile without retrying")
var ErrAttemptCapacity = errors.New("client configuration attempt capacity reached; retained attempts require operator reconciliation")
var ErrAttemptIntent = errors.New("client attempt requires exact original review")

const attemptCapacity = 4096

// Attempts retains dispatch attribution independently of foreign client files.
// A marker is written before external effects and never means success. Once a
// key exists it cannot dispatch again, including after process death. Only
// intent digests are stored; original private review remains with the consumer.
// Verified completion separately retains the original post-apply observation,
// never before-values. Replay repeats admission without inspecting current files.
// New completions retain local caller/action, original scope/revision and time
// in the same synced completion record. Historical completions remain unaudited.
// A foreign-file effect and completion publication are not one transaction;
// incomplete publication remains indeterminate and cannot redispatch effects.
// Markers are not automatically expired: deleting them removes replay protection.
// Admission is capped at 4,096 retained attempts, without automatic eviction.
// Existing replay/recovery remains available at capacity; fresh effects fail.
// TODO: add explicit reconciled cleanup;
// marker presence alone must never acknowledge success.
type Attempts struct {
	directory string
	capacity  int
}

// ApplyResult distinguishes original effect acknowledgement from historical
// result recovery. Replayed retains the original Plan and does not assert that
// current client configuration still matches that committed observation.
type ApplyResult struct {
	Plan
	Replayed bool `json:"replayed"`
}

// ApplyAttempt repeats fresh review and trusted admission before retaining an
// attempt marker and invoking the existing effect owner once. The callback is
// supplied by a trusted consumer, never model text. Completed retries return the
// original verified observation; pending retries remain indeterminate. Replay is
// historical result recovery, not a claim that the current file is configured.
func (service *Service) ApplyAttempt(ctx context.Context, attempts *Attempts, key, rawClient string, reviewed Review, target clientconnect.Target, authorize func() error) (ApplyResult, error) {
	if attempts == nil || authorize == nil {
		return ApplyResult{}, errors.New("client attempt requires durable attribution and trusted admission")
	}
	if err := authorize(); err != nil {
		return ApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if result, found, err := attempts.completed(key, rawClient, reviewed, target); err != nil || found {
		if err != nil {
			if errors.Is(err, ErrAttemptIndeterminate) {
				err = service.credentialRecovery(err, key, target)
			}
			return ApplyResult{}, err
		}
		return ApplyResult{Plan: result, Replayed: true}, nil
	}
	current, err := service.Plan(ctx, rawClient, target)
	if err != nil {
		return ApplyResult{}, err
	}
	if reviewed.Revision == "" {
		return ApplyResult{}, ErrReviewRequired
	}
	if current.Revision != reviewed.Revision || !reviewMatches(reviewed, current) {
		return ApplyResult{Plan: current}, ErrReviewChanged
	}
	if err := authorize(); err != nil {
		return ApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if err := attempts.Begin(key, rawClient, reviewed, target); err != nil {
		return ApplyResult{}, err
	}
	result, err := service.apply(ctx, rawClient, reviewed, target, credentialIssuanceKey(key), authorize)
	if err != nil {
		return ApplyResult{Plan: result}, service.credentialRecovery(err, key, target)
	}
	if err := attempts.complete(key, rawClient, reviewed, target, result); err != nil {
		return ApplyResult{}, service.credentialRecovery(errors.Join(ErrAttemptIndeterminate, err), key, target)
	}
	return ApplyResult{Plan: result}, nil
}

// OpenAttempts opens an existing local attempt directory. Idempotency and
// reviewed admission are enforced by attempt records, not filesystem ACLs.
func OpenAttempts(directory string) (*Attempts, error) {
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("client attempts require a directory")
	}
	return &Attempts{directory: directory, capacity: attemptCapacity}, nil
}

// Begin durably marks original intent before a consumer may dispatch Apply.
// Any error forbids dispatch. A matching retained marker remains indeterminate,
// not permission to retry. Publication or sync failure deliberately leaves the
// marker in place because its durability/effect boundary cannot be inferred.
func (attempts *Attempts) Begin(key, rawClient string, reviewed Review, target clientconnect.Target) error {
	path, expected, err := attempts.identity(key, rawClient, reviewed, target)
	if err != nil {
		return err
	}
	lock, err := attempts.admissionLock()
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return inspectAttempt(path, expected)
	}
	entries, err := os.ReadDir(attempts.directory)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".attempt") {
			count++
		}
	}
	if count >= attempts.capacity {
		return ErrAttemptCapacity
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return inspectAttempt(path, expected)
	}
	if err != nil {
		return err
	}
	return writeAttempt(file, attempts.directory, expected)
}

func (attempts *Attempts) identity(key, rawClient string, reviewed Review, target clientconnect.Target) (string, []byte, error) {
	if len(key) < 16 || len(key) > 256 {
		return "", nil, errors.New("client attempt requires a 16–256 byte retry key")
	}
	if err := target.ValidateLocal(); err != nil {
		return "", nil, err
	}
	clientID, err := clientconnect.ParseClientID(rawClient)
	if err != nil {
		return "", nil, err
	}
	if reviewed.Authentication.Principal != target.Principal() || reviewed.ConfigPaths == nil || reviewed.Changes == nil || reviewed.Revision != reviewRevision(target, clientID, reviewed) {
		return "", nil, ErrAttemptIntent
	}
	keyDigest := sha256.Sum256([]byte("local-operator\x00POST\x00/api/v1/client-configurations\x00" + key))
	intentDigest := sha256.Sum256([]byte(reviewed.Revision))
	expected := fmt.Appendf(nil, "connect-attempt-v1\n%x\n", intentDigest)
	path := filepath.Join(attempts.directory, fmt.Sprintf("%x.attempt", keyDigest))
	return path, expected, nil
}

func writeAttempt(file *os.File, directoryPath string, expected []byte) error {
	written, writeErr := file.Write(expected)
	if writeErr == nil && written != len(expected) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	directory, err := os.Open(directoryPath)
	if err != nil {
		return err
	}
	return errors.Join(atomicfile.SyncDirectory(directory), directory.Close())
}

func inspectAttempt(path string, expected []byte) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return ErrAttemptIndeterminate
	}
	file, err := os.Open(path)
	if err != nil {
		return ErrAttemptIndeterminate
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrAttemptIndeterminate
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil {
		return ErrAttemptIndeterminate
	}
	if !bytes.Equal(raw, expected) {
		return ErrAttemptConflict
	}
	return ErrAttemptIndeterminate
}
