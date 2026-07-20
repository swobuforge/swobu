package responses

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type recordingDecisionSink struct{ effects []compat.Decision }

func (s *recordingDecisionSink) Commit(_ context.Context, _ string, effects []compat.Decision) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func testResponsesPrevious(swobuID, providerID string) *canonical.ResponseRef {
	return &canonical.ResponseRef{
		SwobuID: canonical.NewSwobuResponseID(swobuID),
		Responses: &canonical.ResponsesNativeRef{
			ProviderResponseID: canonical.NewResponsesNativeResponseID(providerID),
			TargetID:           "target", TargetVersion: 1,
		},
	}
}
