package connections

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestSearchFallbackReferencesCountAsLiveConsumers(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspaceWithSearchRoute(slug, nil, nil, nil, "cn_primary", []string{"cn_second", "cn_last"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := routing.NewConfig([]routing.Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := ParseEndpoint("https://search.example.test/search")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewSearchSpec("fixture", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	makeConn := func(raw string) Connection {
		t.Helper()
		id, err := connectionidentity.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		value, err := New(id, raw, spec, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	primary, second, last := makeConn("cn_primary"), makeConn("cn_second"), makeConn("cn_last")
	for _, value := range []Connection{primary, second, last} {
		consumers := References(config, value)
		if len(consumers) != 1 || consumers[0].Workspace != "research" || consumers[0].Kind != Search {
			t.Fatalf("%s consumer missing: %#v", value.ID(), consumers)
		}
	}
	if err := ValidateRoutingReferences(config, []Connection{primary, second, last}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRoutingReferences(config, []Connection{primary, second}); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "fallback_connection_ids[1]") {
		t.Fatalf("missing fallback not rejected at its exact path: %v", err)
	}
}
