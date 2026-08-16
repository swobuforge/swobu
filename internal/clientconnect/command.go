package clientconnect

import "fmt"

func commandClientPresent(binary string) func(*Service) (bool, error) {
	return func(s *Service) (bool, error) {
		_, err := s.lookPath(binary)
		return err == nil, nil
	}
}

func requireCommandOutput(s *Service, client, binary string, args ...string) ([]byte, error) {
	stdout, code, err := s.run(binary, args...)
	if err != nil {
		return nil, fmt.Errorf("%s could not start its configuration command", client)
	}
	if code != 0 {
		return nil, fmt.Errorf("configuration command exited %d", code)
	}
	return stdout, nil
}
