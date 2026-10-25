package operatorclient

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientConfigurationRecoveryPreservesOriginalBytes(t *testing.T) {
	original, _ := connectIntentFixture(t)
	record, err := original.RecoveryRecord()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreClientConfigurationIntent(record)
	if err != nil {
		t.Fatal(err)
	}
	record[0] = 'x'
	review, err := restored.Review()
	if err != nil || review.Workspace != "research" || review.Client.ID != "codex" || review.Changes[0].Before != "private-original" || !review.RequiresReplace {
		t.Fatal("original review lost")
	}
	review.Changes[0].Before = "changed"
	review.ConfigPaths[0] = "changed"
	transport := principalCreationTransport(func(request *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(request.Body)
		if string(raw) != original.body || request.GetBody != nil || request.Header.Get("If-Match") != `"`+original.revision+`"` || request.Header.Get("Idempotency-Key") != original.key {
			t.Fatal("restored send changed original intent")
		}
		return &http.Response{StatusCode: http.StatusConflict, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	if _, err := New(&http.Client{Transport: transport}, "http://fixture").ApplyClientConfiguration(t.Context(), restored); err == nil {
		t.Fatal("denial acknowledged as success")
	}
	recovered, err := restored.RecoveryRecord()
	before, _ := original.RecoveryRecord()
	if err != nil || !bytes.Equal(recovered, before) {
		t.Fatal("detached review changed stored intent")
	}
}

func TestClientConfigurationRecoveryRejectsMalformedRecords(t *testing.T) {
	intent, _ := connectIntentFixture(t)
	record, _ := intent.RecoveryRecord()
	valid := string(record)
	for _, invalid := range []string{
		"", "{}", "null", " " + valid, valid + "{}",
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"approved":true`, 1),
		strings.Replace(valid, `"original-connect-key"`, `""`, 1),
		strings.Replace(valid, `"workspace":"research"`, `"workspace":"research","workspace":"research"`, 1),
		strings.Replace(valid, `"workspace":"research"`, `"workspace":"Research"`, 1),
		strings.Replace(valid, `"client":"codex"`, `"client":"unsupported"`, 1),
		strings.Replace(valid, `"before_exists":true`, `"before_exists":null`, 1),
		strings.Replace(valid, `"before_exists":true,`, "", 1),
		strings.Replace(valid, `"before":"private-original"`, `"before":null`, 1),
		strings.Replace(valid, `"changes":[`, `"approved":true,"changes":[`, 1),
		strings.Replace(valid, `"changes":[`, `"Changes":[`, 1),
		strings.Repeat(" ", 1<<20+1),
	} {
		if restored, err := RestoreClientConfigurationIntent([]byte(invalid)); err == nil || restored.body != "" || err.Error() != "invalid Connect recovery record" {
			t.Fatal("malformed record restored or private data disclosed")
		}
	}
	if _, err := (ClientConfigurationIntent{}).RecoveryRecord(); err == nil {
		t.Fatal("empty intent exported")
	}
	if _, err := (ClientConfigurationIntent{}).Review(); err == nil {
		t.Fatal("empty intent reviewed")
	}
}

func TestClientConfigurationRecoveryPreservesExplicitEmptyEvidence(t *testing.T) {
	_, plan := connectIntentFixture(t)
	plan.Changes[0].Before = ""
	plan.Changes[0].BeforeExists = false
	plan.Changes[0].After = ""
	plan.RequiresReplace = false
	intent, err := NewClientConfigurationIntent(plan, "original-connect-key")
	if err != nil {
		t.Fatal(err)
	}
	record, err := intent.RecoveryRecord()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreClientConfigurationIntent(record); err != nil {
		t.Fatal("explicit empty evidence rejected")
	}
	for _, member := range []string{`"before":"",`, `"before_exists":false,`, `,"after":""`} {
		if _, err := RestoreClientConfigurationIntent([]byte(strings.Replace(string(record), member, "", 1))); err == nil {
			t.Fatal("omitted empty evidence accepted")
		}
	}
	plan.Changes[0].Before = strings.Repeat("private", 1<<18)
	intent, err = NewClientConfigurationIntent(plan, "original-connect-key")
	if err != nil {
		t.Fatal(err)
	}
	if record, err := intent.RecoveryRecord(); err == nil || record != nil {
		t.Fatal("unrestorable oversized record exported")
	}
}
