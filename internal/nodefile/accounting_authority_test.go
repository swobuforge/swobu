package nodefile

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"
)

func TestAccountingPolicySharesAtomicNodeDocumentWithOtherOwners(t *testing.T) {
	f, path := seed(t)
	policy := []byte(`{"schema_version":1,"limits":{}}`)
	if err := f.PublishAccounting(policy); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func() error{
		func() error { return f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)) },
		func() error { return f.PublishPrincipals([]byte(principalDoc)) },
		func() error { return f.PublishPublications([]byte(publicationDoc)) },
		func() error { return f.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\n")) },
	} {
		if err := mutation(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := f.AccountingDocument()
	if err != nil || !bytes.Equal(raw, policy) {
		t.Fatalf("lost accounting policy %s %v", raw, err)
	}
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\naccounting: {}\n")); err == nil {
		t.Fatal("routing owner overwrote accounting policy")
	}
	_, rev, err := f.Snapshot()
	if err != nil || len(rev) != 64 {
		t.Fatal("no complete revision", err)
	}
	// The failed write must pass reference admission so the injected disk
	// failure, not schema validation, is what proves atomic rollback.
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n")); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	f.replace = func(string, []byte, os.FileMode) error { return errors.New("disk failure") }
	if err := f.PublishAccounting([]byte(`{"schema_version":1,"limits":{"research":{"currency":"USD","amount_cents":100,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`)); err == nil {
		t.Fatal("accepted failed commit")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("non-atomic failed policy update")
	}
}

func TestConcurrentAccountingAndOtherNodeWritersPreserveBoth(t *testing.T) {
	f, _ := seed(t)
	var wg sync.WaitGroup
	errorsCh := make(chan error, 4)
	for _, mutation := range []func() error{
		func() error { return f.PublishAccounting([]byte(`{"schema_version":1,"limits":{}}`)) },
		func() error { return f.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)) },
		func() error { return f.PublishPrincipals([]byte(principalDoc)) },
		func() error { return f.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\n")) },
	} {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if err := fn(); err != nil {
					errorsCh <- err
					return
				}
			}
		}(mutation)
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	for _, section := range []func() ([]byte, error){f.AccountingDocument, f.ConnectionDocument, f.PrincipalDocument} {
		raw, err := section()
		if err != nil || len(raw) == 0 {
			t.Fatalf("lost concurrent section: %s %v", raw, err)
		}
	}
}

func TestAccountingLimitBlocksWorkspaceRemovalUntilPolicyIsCleared(t *testing.T) {
	f, _ := seed(t)
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n")); err != nil {
		t.Fatal(err)
	}
	active := []byte(`{"schema_version":1,"limits":{"research":{"currency":"USD","amount_cents":100,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`)
	if err := f.PublishAccounting(active); err != nil {
		t.Fatal(err)
	}
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\n")); err == nil {
		t.Fatal("removed budgeted Workspace")
	}
	if err := f.PublishAccounting([]byte(`{"schema_version":1,"limits":{}}`)); err != nil {
		t.Fatal(err)
	}
	if err := f.PublishRouting([]byte("schema_version: 5\nworkspaces: {}\n")); err != nil {
		t.Fatal("Workspace delete still blocked", err)
	}
}
