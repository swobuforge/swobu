package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func TestConnectionBindingFreezesRevisionAndCredential(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		if r.URL.Query().Get("q") != "synthetic query" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("number_of_results") != "1" || r.URL.Fragment != "" {
			t.Error("Connection binding changed Search query semantics")
		}
		w.Write([]byte(`{"results":[{"url":"https://example.com","title":"Synthetic"}]}`))
	}))
	defer server.Close()
	original := searchCheckConnection(t, server.URL, "env:ORIGINAL")
	// The captured Connection contains the current clean Search endpoint.
	resolve := func(_ context.Context, scope credentialref.Scope, ref string) (string, error) {
		if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != original.ID().String() {
			t.Fatal("credential owner changed")
		}
		return ref, nil
	}
	first, err := OpenConnectionBinding(t.Context(), original, resolve, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := original.ReplaceCredential(original.Revision(), "", credentialref.Parse("env:ORIGINAL"), credentialref.Parse("env:ROTATED"), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenConnectionBinding(t.Context(), rotated, resolve, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []ConnectionBinding{first, second, first} {
		results, err := binding.Search(t.Context(), Query{Text: "synthetic query", Limit: 1})
		if err != nil || len(results) != 1 {
			t.Fatal("Connection query failed", err)
		}
	}
	if first.ConnectionID() != second.ConnectionID() || first.ConnectionRevision().String() != "1" || second.ConnectionRevision().String() != "2" || strings.Join(tokens, ",") != "Bearer env:ORIGINAL,Bearer env:ROTATED,Bearer env:ORIGINAL" {
		t.Fatal("rotation rewrote captured execution identity or credential")
	}
}

func TestConnectionBindingRejectsUnavailableCredentialWithoutQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("queried without admitted binding") }))
	defer server.Close()
	value := searchCheckConnection(t, server.URL, "env:SYNTHETIC")
	for _, resolve := range []ConnectionCredentialResolver{nil, func(context.Context, credentialref.Scope, string) (string, error) {
		return "", errors.New("private-secret-error")
	}, func(context.Context, credentialref.Scope, string) (string, error) { return " ", nil }} {
		binding, err := OpenConnectionBinding(t.Context(), value, resolve, server.Client())
		if err == nil || strings.Contains(err.Error(), "private-secret") || binding.ConnectionID().String() != "" {
			t.Fatal("credential failure exposed private error or partial binding")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := OpenConnectionBinding(ctx, value, func(context.Context, credentialref.Scope, string) (string, error) {
		t.Fatal("resolved cancelled binding")
		return "", nil
	}, server.Client()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if _, err := (ConnectionBinding{}).Search(t.Context(), Query{Text: "synthetic"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatal("zero binding executed", err)
	}
}
