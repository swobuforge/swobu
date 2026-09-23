package continuity

import (
	"fmt"
	"math"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const (
	checkpointAnchorWeight = uint64(512)
	historySegmentWeight   = uint64(64)
	canonicalItemFloor     = uint64(96)
	containerFloor         = uint64(32)
)

// This native-object sizing policy exists only while Continuity retains the
// canonical object graph. If the post-M1 byte-container experiment becomes the
// selected implementation, delete this file, canonical's retained-payload
// walker, and their heap-calibration lane together. Do not grow this into a
// general heap-accounting subsystem.

// Weight is a deterministic retained-cost estimate for capacity policy. It is
// deliberately conservative and is not a claim about exact Go heap ownership.
func addWeight(total *uint64, values ...uint64) error {
	for _, value := range values {
		if math.MaxUint64-*total < value {
			return fmt.Errorf("checkpoint retained weight overflows uint64")
		}
		*total += value
	}
	return nil
}

func checkpointLocalWeight(workspace string, checkpoint Checkpoint, preludeWeight, responseWeight uint64) (uint64, error) {
	weight := checkpointAnchorWeight
	response := checkpoint.Response
	ref := response.Response()
	variable := uint64(len(workspace) + len(ref.SwobuID.String()) + len(response.Model()) + len(response.Completion().Reason()))
	if ref.Responses != nil {
		variable += uint64(len(ref.Responses.ProviderResponseID.String()) + len(ref.Responses.TargetID))
	}
	if ref.Interactions != nil {
		variable += uint64(len(ref.Interactions.ProviderInteractionID.String()) + len(ref.Interactions.TargetID))
	}
	if checkpoint.History != nil {
		variable += uint64(len(checkpoint.History.Scheme()))
	}
	requestWeight, err := canonicalRequestMetadataWeight(checkpoint.Request)
	if err != nil {
		return 0, err
	}
	if err := addWeight(&weight, variable, requestWeight, preludeWeight, responseWeight); err != nil {
		return 0, err
	}
	return weight, nil
}

func canonicalRequestMetadataWeight(request canonical.CanonicalRequest) (uint64, error) {
	payload, containers, err := canonical.RetainedRequestMetadataWeight(request)
	if err != nil {
		return 0, err
	}
	weight := containerFloor
	if containers > math.MaxUint64/containerFloor {
		return 0, fmt.Errorf("checkpoint retained weight overflows uint64")
	}
	if err := addWeight(&weight, payload, containers*containerFloor); err != nil {
		return 0, err
	}
	return weight, nil
}

func canonicalItemsWeight(items []canonical.CanonicalItem) (uint64, error) {
	weight := containerFloor
	for _, item := range items {
		itemWeight, err := canonicalItemWeight(item)
		if err != nil {
			return 0, err
		}
		if err := addWeight(&weight, itemWeight); err != nil {
			return 0, err
		}
	}
	return weight, nil
}

func canonicalItemWeights(items []canonical.CanonicalItem) ([]uint64, error) {
	weights := make([]uint64, len(items))
	for index, item := range items {
		weight, err := canonicalItemWeight(item)
		if err != nil {
			return nil, err
		}
		weights[index] = weight
	}
	return weights, nil
}

func historySegmentRetainedWeight(itemWeights []uint64) uint64 {
	if len(itemWeights) == 0 {
		return 0
	}
	weight := historySegmentWeight + containerFloor
	for _, itemWeight := range itemWeights {
		weight += itemWeight
	}
	return weight
}

func canonicalItemWeight(item canonical.CanonicalItem) (uint64, error) {
	weight := canonicalItemFloor
	payload, containers, err := canonical.RetainedPayloadWeight(item)
	if err != nil {
		return 0, err
	}
	if containers > math.MaxUint64/containerFloor {
		return 0, fmt.Errorf("checkpoint retained weight overflows uint64")
	}
	if err := addWeight(&weight, payload, containers*containerFloor); err != nil {
		return 0, err
	}
	return weight, nil
}
