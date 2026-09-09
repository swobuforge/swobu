package wire

import "github.com/swobuforge/swobu/internal/domain/canonical"

// SchemaContractExact reports whether an established contract family can be
// preserved by one target profile. Unprofiled input is never evidence of
// exactness.
func SchemaContractExact(contract canonical.SchemaContract, target canonical.SchemaProfile) bool {
	return contract.Profile != canonical.SchemaProfileUnspecified &&
		contract.Profile != canonical.SchemaProfileUnprofiled &&
		contract.Profile == target
}
