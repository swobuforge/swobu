package connect

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/clientconnect"
)

func TestApplyResultEncodesExplicitFreshAndReplayedDecision(t *testing.T) {
	for _, replayed := range []bool{false, true} {
		raw, err := json.Marshal(ApplyResult{Replayed: replayed})
		if err != nil {
			t.Fatal(err)
		}
		var acknowledgement struct {
			Replayed *bool `json:"replayed"`
		}
		if json.Unmarshal(raw, &acknowledgement) != nil || acknowledgement.Replayed == nil || *acknowledgement.Replayed != replayed {
			t.Fatal("apply result omitted explicit replay decision")
		}
	}
}

func TestCompletedAttemptReplaysOriginalResultWithoutReadingOrApplyingClient(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	attempts, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	target, plan := testPlan(t)
	verified := plan
	verified.Changes = nil
	clients := &fakeClients{plan: plan, applyPlan: verified}
	service, err := NewService(clients, testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	preview := projectPlan(target, plan)
	review := preview.Review()
	const key = "completed-original-key"
	result, err := service.ApplyAttempt(t.Context(), attempts, key, "codex", review, target, func() error { return nil })
	if err != nil || !result.AlreadyConfigured || result.Replayed || !clients.applied {
		t.Fatal("verified first apply did not retain completion")
	}
	clients.applied = false
	clients.plan = clientconnect.Plan{}
	reopened, err := OpenAttempts(directory)
	if err != nil {
		t.Fatal(err)
	}
	reopened.capacity = 1
	replay, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return nil })
	if err != nil || !replay.Replayed || !reflect.DeepEqual(result.Plan, replay.Plan) || clients.applied {
		t.Fatal("completed replay refreshed result or repeated effect")
	}
	denied := errors.New("admission revoked")
	if _, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("receipt bypassed current admission")
	}
	path, _, err := reopened.identity(key, "codex", review, target)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(path + ".completed")
	if err != nil {
		t.Fatal(err)
	}
	var committed completion
	if json.Unmarshal(valid, &committed) != nil || committed.Audit == nil || !validCompletionAudit(committed.Audit, committed.Result, committed.Intent) || committed.Audit.Revision != review.Revision {
		t.Fatal("verified completion omitted original checked audit")
	}
	audits, err := reopened.Audits()
	if err != nil || len(audits) != 1 || audits[0] != *committed.Audit {
		t.Fatal("audit read lost original completion", err)
	}
	audits[0].Caller = "tampered"
	audits, err = reopened.Audits()
	if err != nil || len(audits) != 1 || audits[0].Caller != "local" {
		t.Fatal("audit read aliases retained facts", err)
	}
	reopened.capacity = 3
	if err := reopened.Begin("pending-original-key", "codex", review, target); err != nil {
		t.Fatal(err)
	}
	if facts, err := reopened.Audits(); err != nil || len(facts) != 1 {
		t.Fatal("pending attempt acquired completion facts", err)
	}
	for _, malformed := range [][]byte{
		bytes.Replace(valid, []byte(`"caller":"local"`), []byte(`"caller":"forged"`), 1),
		bytes.Replace(valid, []byte(`"workspace":"dev"`), []byte(`"workspace":"foreign"`), 1),
		bytes.Replace(valid, []byte(`"client_configuration.apply"`), []byte(`"other"`), 1),
		bytes.Replace(valid, []byte(`"caller":"local"`), []byte(`"caller":null`), 1),
		bytes.Replace(valid, []byte(`"caller":"local"`), []byte(`"caller":"local","caller":"local"`), 1),
		bytes.Replace(valid, []byte(`"caller":"local",`), nil, 1),
		bytes.Replace(valid, []byte(`"committed_at":"`), []byte(`"committed_at":"invalid`), 1),
		bytes.Replace(valid, []byte(`"audit":{`), []byte(`"audit":{"unknown":true,`), 1),
		bytes.Replace(valid, []byte(`"audit":{`), []byte(`"audit":null,"audit":{`), 1),
	} {
		if err := os.WriteFile(path+".completed", malformed, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return nil }); !errors.Is(err, ErrAttemptIndeterminate) || clients.applied {
			t.Fatal("invalid audit acknowledged or redispatched", err)
		}
		if facts, err := reopened.Audits(); err == nil || facts != nil {
			t.Fatal("damaged completion projected audit", err)
		}
	}
	committed.Audit = nil
	historical, err := json.Marshal(committed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".completed", historical, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return nil }); err != nil || clients.applied {
		t.Fatal("historical completion cannot replay", err)
	}
	unchanged, err := os.ReadFile(path + ".completed")
	if err != nil || !bytes.Equal(historical, unchanged) {
		t.Fatal("historical replay invented audit", err)
	}
	if facts, err := reopened.Audits(); err != nil || len(facts) != 0 {
		t.Fatal("historical completion acquired audit", err)
	}
	explicitNull := bytes.Replace(historical, []byte(`{"intent"`), []byte(`{"audit":null,"intent"`), 1)
	if err := os.WriteFile(path+".completed", explicitNull, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return nil }); !errors.Is(err, ErrAttemptIndeterminate) {
		t.Fatal("explicit null audit admitted as historical omission", err)
	}
	for _, malformed := range [][]byte{
		append(append([]byte(nil), valid...), []byte(` {}`)...),
		bytes.Replace(valid, []byte(`"already_configured":true`), []byte(`"already_configured":false`), 1),
		bytes.Replace(valid, []byte(`"requires_replace":false`), []byte(`"requires_replace":null`), 1),
		bytes.Replace(valid, []byte(`"already_configured":true`), []byte(`"already_configured":true,"already_configured":true`), 1),
		bytes.Replace(valid, []byte(`{"intent"`), []byte(`{"unknown":true,"intent"`), 1),
	} {
		if err := os.WriteFile(path+".completed", malformed, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return nil }); !errors.Is(err, ErrAttemptIndeterminate) || clients.applied {
			t.Fatal("malformed completion acknowledged or dispatched")
		}
	}
	if err := os.WriteFile(path+".completed", []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyAttempt(t.Context(), reopened, key, "codex", review, target, func() error { return nil }); !errors.Is(err, ErrAttemptIndeterminate) || clients.applied {
		t.Fatal("corrupt completion acknowledged or dispatched")
	}
}

func TestRealFileCompletedReplayPreservesLaterOperatorChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", root)
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"original\"\n[model_providers.swobu.http_headers]\nAuthorization=\"Bearer fixture-client-token\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	attempts, err := OpenAttempts(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(clientconnect.NewService(), testCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := clientconnect.NewTarget("research", "http://127.0.0.1:7926/workspaces/research")
	if err != nil {
		t.Fatal(err)
	}
	target = target.WithModelID("coding").WithAuthentication("structural-test-principal", "")
	preview, err := service.Plan(t.Context(), "codex", target)
	if err != nil {
		t.Fatal(err)
	}
	review := preview.Review()
	result, err := service.ApplyAttempt(t.Context(), attempts, "real-completed-key", "codex", review, target, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	const later = "model = \"later-operator-choice\"\n"
	if err := os.WriteFile(path, []byte(later), 0600); err != nil {
		t.Fatal(err)
	}
	replay, err := service.ApplyAttempt(t.Context(), attempts, "real-completed-key", "codex", review, target, func() error { return nil })
	if err != nil || result.Replayed || !replay.Replayed || !reflect.DeepEqual(result.Plan, replay.Plan) {
		t.Fatal("replay did not retain original committed observation")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != later {
		t.Fatal("completed replay overwrote later operator choice")
	}
}
