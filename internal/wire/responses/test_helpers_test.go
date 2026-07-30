package responses

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type recordingChanges = []compat.Change

func testResponsesPrevious(swobuID, providerID string) *canonical.ResponseRef {
	return &canonical.ResponseRef{
		SwobuID: canonical.NewSwobuResponseID(swobuID),
		Responses: &canonical.ResponsesContinuation{
			ProviderResponseID: canonical.NewResponsesResponseID(providerID),
			TargetID:           "target", TargetVersion: 1,
		},
	}
}
