package clientconnect

import (
	"context"
	"fmt"
	"os"
)

type adapter struct {
	id             ClientID
	name           string
	targetOptional bool
	present        func(*Service) (bool, error)
	planCurrent    func(context.Context, *Service, Target) (plannedMutation, error)
}

func ClientRequiresTarget(id ClientID) (bool, error) {
	adapter, ok := adapterFor(id)
	if !ok {
		return false, fmt.Errorf("unsupported client")
	}
	return !adapter.targetOptional, nil
}

var adapters = []adapter{
	codexAdapter,
	claudeAdapter,
	kiloAdapter,
	openCodeAdapter,
	piAdapter,
	museAdapter,
	openClawAdapter,
	hermesAdapter,
	antigravityAdapter,
}

func adapterFor(id ClientID) (adapter, bool) {
	for _, candidate := range adapters {
		if candidate.id == id {
			return candidate, true
		}
	}
	return adapter{}, false
}

func binaryOrRegularFilePresent(s *Service, binary string, paths ...string) (bool, error) {
	if _, err := s.lookPath(binary); err == nil {
		return true, nil
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil {
			if info.Mode().IsRegular() {
				return true, nil
			}
			continue
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}
