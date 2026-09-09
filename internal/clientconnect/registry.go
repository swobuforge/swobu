package clientconnect

import (
	"context"
	"os"
)

type adapter struct {
	id          ClientID
	name        string
	present     func(*Service) (bool, error)
	planCurrent func(context.Context, *Service, Target) (plannedMutation, error)
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
