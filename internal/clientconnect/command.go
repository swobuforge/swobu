package clientconnect

import (
	"context"
	"fmt"
	"strings"
)

func commandClientPresent(binary string) func(*Service) (bool, error) {
	return func(s *Service) (bool, error) {
		_, err := s.lookPath(binary)
		return err == nil, nil
	}
}

func requireCommandOutput(ctx context.Context, s *Service, client, binary string, args ...string) ([]byte, error) {
	output, code, err := s.run(ctx, binary, args...)
	if err != nil {
		return nil, fmt.Errorf("%s could not start its configuration command: %w", client, err)
	}
	if code != 0 {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return nil, fmt.Errorf("%s", detail)
		}
		return nil, fmt.Errorf("configuration command exited %d", code)
	}
	return output, nil
}
