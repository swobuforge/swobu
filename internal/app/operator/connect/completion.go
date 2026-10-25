package connect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

type completion struct {
	Intent string `json:"intent"`
	Result Plan   `json:"result"`
	Audit  *Audit `json:"audit,omitempty"`
}

// Audit records verified client-configuration completion, not current foreign
// file readiness. It contains no original file values, retry key or approval.
type Audit struct {
	Action      string    `json:"action"`
	Caller      string    `json:"caller"`
	Workspace   string    `json:"workspace"`
	Client      string    `json:"client"`
	Revision    string    `json:"revision"`
	CommittedAt time.Time `json:"committed_at"`
}

func validCompletionAudit(audit *Audit, result Plan, identity string) bool {
	if audit == nil {
		return true
	}
	intentDigest := sha256.Sum256([]byte(audit.Revision))
	revision, err := hex.DecodeString(strings.TrimPrefix(audit.Revision, "connect-"))
	return audit.Action == "client_configuration.apply" && audit.Caller == "local" &&
		audit.Workspace == result.Workspace && audit.Client == result.Client.ID &&
		err == nil && len(revision) == sha256.Size && audit.Revision == "connect-"+hex.EncodeToString(revision) && !audit.CommittedAt.IsZero() &&
		identity == fmt.Sprintf("connect-attempt-v1\n%x\n", intentDigest)
}

func (attempts *Attempts) complete(key, rawClient string, review Review, target clientconnect.Target, result Plan) error {
	path, identity, err := attempts.identity(key, rawClient, review, target)
	if err != nil {
		return err
	}
	if !validCompletion(result, rawClient, review, target) || !exactAttempt(path, identity) {
		return ErrAttemptIndeterminate
	}
	audit := &Audit{Action: "client_configuration.apply", Caller: "local", Workspace: target.WorkspaceSlug(), Client: rawClient, Revision: review.Revision, CommittedAt: time.Now().UTC()}
	raw, err := json.Marshal(completion{Intent: string(identity), Result: result, Audit: audit})
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return ErrAttemptIndeterminate
	}
	file, err := os.OpenFile(path+".completed", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return writeAttempt(file, attempts.directory, raw)
}

func (attempts *Attempts) completed(key, rawClient string, review Review, target clientconnect.Target) (Plan, bool, error) {
	path, identity, err := attempts.identity(key, rawClient, review, target)
	if err != nil {
		return Plan{}, false, err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if _, completionErr := os.Lstat(path + ".completed"); !errors.Is(completionErr, os.ErrNotExist) {
			return Plan{}, false, ErrAttemptIndeterminate
		}
		return Plan{}, false, nil
	}
	if err := inspectAttempt(path, identity); !errors.Is(err, ErrAttemptIndeterminate) {
		return Plan{}, false, err
	}
	if !exactAttempt(path, identity) {
		return Plan{}, false, ErrAttemptIndeterminate
	}
	record, err := readCompletion(path)
	if err != nil || record.Intent != string(identity) || !validCompletion(record.Result, rawClient, review, target) {
		return Plan{}, false, ErrAttemptIndeterminate
	}
	return record.Result, true, nil
}

func readCompletion(path string) (completion, error) {
	info, err := os.Lstat(path + ".completed")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return completion{}, ErrAttemptIndeterminate
	}
	file, err := os.Open(path + ".completed")
	if err != nil {
		return completion{}, ErrAttemptIndeterminate
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return completion{}, ErrAttemptIndeterminate
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return completion{}, ErrAttemptIndeterminate
	}
	var record completion
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || !validCompletionAudit(record.Audit, record.Result, record.Intent) || !exactAttempt(path, []byte(record.Intent)) {
		return completion{}, ErrAttemptIndeterminate
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(raw, canonical) {
		return completion{}, ErrAttemptIndeterminate
	}
	return record, nil
}

// Audits reads matched verified completions without inspecting foreign files.
// Pending and historical records contribute no facts. Damaged completion evidence
// rejects the entire read rather than silently returning incomplete success facts.
func (attempts *Attempts) Audits() ([]Audit, error) {
	entries, err := os.ReadDir(attempts.directory)
	if err != nil {
		return nil, err
	}
	result := []Audit{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".attempt.completed") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".attempt.completed")
		digest, err := hex.DecodeString(name)
		if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != name {
			return nil, ErrAttemptIndeterminate
		}
		record, err := readCompletion(filepath.Join(attempts.directory, name+".attempt"))
		if err != nil {
			return nil, err
		}
		if record.Audit == nil {
			continue
		}
		slug, scopeErr := routing.ParseWorkspaceSlug(record.Audit.Workspace)
		_, clientErr := clientconnect.ParseClientID(record.Audit.Client)
		if scopeErr != nil || slug.String() != record.Audit.Workspace || clientErr != nil || record.Result.Changes == nil || len(record.Result.Changes) != 0 || !record.Result.AlreadyConfigured || record.Result.RequiresReplace || record.Result.ConfigPaths == nil || record.Result.Revision == "" {
			return nil, ErrAttemptIndeterminate
		}
		result = append(result, *record.Audit)
	}
	return result, nil
}

func exactAttempt(path string, expected []byte) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	return err == nil && bytes.Equal(raw, expected)
}

func validCompletion(result Plan, rawClient string, review Review, target clientconnect.Target) bool {
	if result.Workspace != target.WorkspaceSlug() || result.Client.ID != rawClient || result.Changes == nil || len(result.Changes) != 0 || !result.AlreadyConfigured || result.RequiresReplace || !reviewMatches(Review{ConfigPaths: review.ConfigPaths, Changes: []Change{}, Authentication: result.Authentication}, result) || result.Authentication.Principal != review.Authentication.Principal || result.Authentication.IssueCredential {
		return false
	}
	return result.Revision == reviewRevision(target, clientconnect.ClientID(rawClient), result.Review())
}
