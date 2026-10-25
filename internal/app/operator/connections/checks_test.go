package connections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestCheckTransactionAdmissionRevisionAndEphemeralPublication(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	registry, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	commands, _ := NewCommands(registry, lifecycle)
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE"))
	admit := func(context.Context) error { return nil }
	created, _, err := commands.Create(t.Context(), "Shared", spec, "local", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse(created.ID)
	revision, _ := domain.ParseRevision(created.Revision)
	before, _ := os.ReadFile(path)
	probes := 0
	probe := func(ctx context.Context, value domain.Connection) (domain.Check, error) {
		probes++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > CheckTimeout {
			t.Fatal("probe unbounded")
		}
		// A registry read inside the probe proves it runs outside publication locks.
		if _, err := registry.Get(id); err != nil {
			t.Fatal(err)
		}
		return domain.NewCheck(value, time.Now(), domain.Reachable, domain.AuthenticationAccepted, []domain.Capability{domain.CapabilityTools}, domain.CheckOK)
	}
	result, err := commands.Check(t.Context(), id, revision, admit, probe)
	if err != nil || result.ConnectionRevision != "1" || result.Reachability != domain.Reachable {
		t.Fatal("check failed", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(registry.Audits()) != 1 {
		t.Fatal("check changed configuration/audit")
	}
	reopened, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.LatestCheck(id); ok {
		t.Fatal("observation persisted")
	}
	denied := errors.New("denied")
	if _, err := commands.Check(t.Context(), id, revision, func(context.Context) error { return denied }, probe); !errors.Is(err, denied) || probes != 1 {
		t.Fatal("denied check ran probe")
	}
	calls := 0
	if _, err := commands.Check(t.Context(), id, revision, func(context.Context) error {
		calls++
		if calls > 1 {
			return denied
		}
		return nil
	}, probe); !errors.Is(err, denied) {
		t.Fatal("withdrawn publication accepted")
	}
	errProbe := func(context.Context, domain.Connection) (domain.Check, error) {
		return domain.Check{}, errors.New("synthetic-private-backend")
	}
	if _, err := commands.Check(t.Context(), id, revision, admit, errProbe); err == nil || strings.Contains(err.Error(), "synthetic-private") {
		t.Fatal("probe error escaped")
	}
	editingProbe := func(ctx context.Context, value domain.Connection) (domain.Check, error) {
		if _, err := commands.EditSameIdentity(ctx, id, revision, "Edited", spec, true, "local", admit); err != nil {
			t.Fatal(err)
		}
		return domain.NewCheck(value, time.Now(), domain.Reachable, domain.AuthenticationUnknown, nil, domain.CheckOK)
	}
	if _, err := commands.Check(t.Context(), id, revision, admit, editingProbe); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatal("stale completion accepted", err)
	}
	if _, ok := registry.LatestCheck(id); ok {
		t.Fatal("old revision health visible")
	}
	if _, err := commands.Check(t.Context(), id, revision, admit, probe); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatal("stale inspection accepted")
	}
}

func TestCheckCanceledCallerCannotPublishObservation(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(dir, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	commands, _ := NewCommands(registry, lifecycle)
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE"))
	admit := func(context.Context) error { return nil }
	created, _, err := commands.Create(t.Context(), "Shared", spec, "local", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse(created.ID)
	revision, _ := domain.ParseRevision(created.Revision)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := func(ctx context.Context, value domain.Connection) (domain.Check, error) {
		cancel()
		<-ctx.Done()
		return domain.NewCheck(value, time.Now(), domain.Reachable, domain.AuthenticationUnknown, nil, domain.CheckOK)
	}
	if _, err := commands.Check(ctx, id, revision, admit, probe); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled caller published", err)
	}
	if _, ok := registry.LatestCheck(id); ok {
		t.Fatal("canceled evidence published")
	}
}
