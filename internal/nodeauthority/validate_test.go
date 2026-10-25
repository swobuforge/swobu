package nodeauthority_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/accounting"

	"github.com/swobuforge/swobu/internal/nodeauthority"
	"github.com/swobuforge/swobu/internal/nodefile"
)

const emptyNode = "schema_version: 5\nworkspaces: {}\n"
const researchNode = "schema_version: 5\nworkspaces:\n  research:\n    routes: []\n"
const emptyPrincipals = `{"schema_version":3,"access":[]}`
const emptyPublications = `{"workspaces":[]}`
const emptyPolicy = `{"schema_version":1,"limits":{}}`

func create(t *testing.T, initial string) (*nodefile.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return file, path
}

// Previously Workspace deletion could commit a mixed-generation file and
// rely on a later reconciliation. The Node file must reject the *candidate*
// before publication, including if a concurrent section writer intervenes.

// Publications still block deletion after the last Principal is removed.

func TestAdmissionRejectsInvalidExistingDocumentWithoutRepair(t *testing.T) {
	file, path := create(t, emptyNode)
	// Direct low-level storage setup creates a deliberately invalid document.
	// Installing the application's admission gate must fail without rewriting.
	if err := file.PublishPublications([]byte(`{"workspaces":["missing"]}`)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.InstallAdmission(nodeauthority.Admit); err == nil {
		t.Fatal("accepted orphaned startup publication")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed validation rewrote YAML")
	}
}

func TestConnectionAndAccountingReferencesValidatedTogether(t *testing.T) {
	good := []byte(researchNode)
	if _, err := nodeauthority.Validate(good); err != nil {
		t.Fatal(err)
	}
	withoutConnection := []byte("schema_version: 5\nworkspaces:\n  research:\n    enabled_services: [mcp]\n    routes: []\n    mcp_mounts:\n      github:\n        connection_id: cn_missing\n        allowed_tools: [read_issue]\n")
	if _, err := nodeauthority.Validate(withoutConnection); err == nil {
		t.Fatal("dangling MCP Connection accepted")
	}
	badPolicy := []byte(emptyNode + "accounting:\n  schema_version: 1\n  limits:\n    research:\n      currency: USD\n      amount_cents: 100\n      window: day\n      mode: stop_new_billable_work\n      unknown_price: reject\n")
	if _, err := nodeauthority.Validate(badPolicy); err == nil {
		t.Fatal("dangling budget accepted")
	}
	if _, err := accounting.ValidatePolicyDocument([]byte(emptyPolicy)); err != nil {
		t.Fatal(err)
	}
}

func TestNodePublicationConcurrentIndependentAuthorities(t *testing.T) {
	file, _ := create(t, researchNode)
	if err := file.InstallAdmission(nodeauthority.Admit); err != nil {
		t.Fatal(err)
	}
	work := []func() error{
		func() error { return file.PublishRouting([]byte(researchNode)) },
		func() error { return file.PublishConnections([]byte(`{"schema_version":1,"connections":[]}`)) },
		func() error { return file.PublishPrincipals([]byte(emptyPrincipals)) },
		func() error { return file.PublishPublications([]byte(emptyPublications)) },
		func() error { return file.PublishAccounting([]byte(emptyPolicy)) },
	}
	var wg sync.WaitGroup
	failures := make(chan error, len(work))
	for _, write := range work {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				if err := fn(); err != nil {
					failures <- err
					return
				}
			}
		}(write)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	raw, _, err := file.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := nodeauthority.Validate(raw)
	if err != nil || summary.Workspaces != 1 {
		t.Fatalf("invalid converged Node: %+v %v", summary, err)
	}
}

func TestReadOnlyNodeCanInstallSameAdmissionWithoutWrites(t *testing.T) {
	file, path := create(t, researchNode)
	_ = file
	readOnly, err := nodefile.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := readOnly.InstallAdmission(nodeauthority.Admit); err != nil {
		t.Fatal(err)
	}
	if err := readOnly.PublishPrincipals([]byte(emptyPrincipals)); !errors.Is(err, nodefile.ErrReadOnly) {
		t.Fatalf("read-only write: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only Node mutated", err)
	}
}

// Old authorization documents must fail complete Node validation without repair.
func TestNodeRejectsObsoletePublicationAuthority(t *testing.T) {
	for _, section := range []string{
		"publications:\n  schema_version: 2\n  exposures: []\n",
		"publications:\n  workspaces: []\n  anonymous: true\n",
		"share:\n  grants: []\n",
	} {
		raw := []byte(researchNode + section)
		if _, err := nodeauthority.Validate(raw); err == nil {
			t.Fatalf("obsolete authority accepted: %s", section)
		}
	}
}
