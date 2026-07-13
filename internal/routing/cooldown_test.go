package routing

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCooldownStore_MarkGetClear(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCooldownStore()
	key := TargetKey{Workspace: "dev", Route: "gpt", TargetID: "chatgpt-main"}

	// Initially no cooldown.
	if _, ok := store.Get(ctx, key); ok {
		t.Fatal("expected no cooldown initially")
	}

	// Mark cooldown.
	store.Mark(ctx, key, FailureRateLimited, 30*time.Second)
	state, ok := store.Get(ctx, key)
	if !ok {
		t.Fatal("expected cooldown after mark")
	}
	if state.FailureClass != FailureRateLimited {
		t.Errorf("FailureClass = %v, want %v", state.FailureClass, FailureRateLimited)
	}

	// Clear cooldown.
	store.Clear(ctx, key)
	if _, ok := store.Get(ctx, key); ok {
		t.Fatal("expected no cooldown after clear")
	}
}

func TestMemoryCooldownStore_Expiration(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCooldownStore()
	fixed := time.Now()
	store.now = func() time.Time { return fixed }

	key := TargetKey{Workspace: "dev", Route: "gpt", TargetID: "chatgpt-main"}
	store.Mark(ctx, key, FailureTimeout, 5*time.Second)

	// Should be active.
	if _, ok := store.Get(ctx, key); !ok {
		t.Fatal("expected cooldown to be active")
	}

	// Advance time past expiration.
	store.now = func() time.Time { return fixed.Add(10 * time.Second) }
	if _, ok := store.Get(ctx, key); ok {
		t.Fatal("expected cooldown to have expired")
	}
}

func TestNormalizeCooldownDuration(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  time.Duration
	}{
		{0, DefaultCooldownDuration},
		{-1 * time.Second, DefaultCooldownDuration},
		{10 * time.Second, 10 * time.Second},
		{DefaultCooldownDuration, DefaultCooldownDuration},
		{MaxCooldownDuration, MaxCooldownDuration},
		{MaxCooldownDuration + time.Second, MaxCooldownDuration},
	}
	for _, tt := range tests {
		got := NormalizeCooldownDuration(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeCooldownDuration(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestMemoryCooldownStore_Concurrent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCooldownStore()
	key := TargetKey{Workspace: "dev", Route: "gpt", TargetID: "chatgpt-main"}

	// Start 10 goroutines marking and clearing.
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			store.Mark(ctx, key, FailureRateLimited, 30*time.Second)
			store.Get(ctx, key)
			store.Clear(ctx, key)
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
