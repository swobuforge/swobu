package messages

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func testAttemptToolNames(request canonical.CanonicalRequest) provider.AttemptToolNames {
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		panic(err)
	}
	return names
}
