package exchange

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
)

type rejectingCompatibilitySink struct{}

func (rejectingCompatibilitySink) Commit(context.Context, string, []compat.Decision) error {
	return errors.New("evidence unavailable")
}

func TestCompatibilityEvidencePersistenceIsBestEffort(t *testing.T) {
	commitDecisionsBestEffort(context.Background(), rejectingCompatibilitySink{}, "ex", []compat.Decision{
		{
			Feature: compat.GenerationMaxTokens,
			Outcome: compat.Exact,
			Subject: compat.Subject("provider:openai"),
		},
	})
}
