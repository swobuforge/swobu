package connections

import "github.com/swobuforge/swobu/internal/domain/connectionidentity"

// Snapshot captures configuration and optional current-revision health under
// one registry read lock. Values are detached; mutating returned slices cannot
// affect later reads. This is internal state, not an HTTP response DTO.
type Snapshot struct {
	Connection  Connection
	LatestCheck *Check
}

func (s *Store) snapshotLocked(value Connection) Snapshot {
	result := Snapshot{Connection: value}
	if check, ok := s.checks[value.ID()]; ok && check.revision == value.revision {
		check.capabilities = check.Capabilities()
		result.LatestCheck = &check
	}
	return result
}

func (s *Store) Snapshot(id connectionidentity.ID) (Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[id]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	return s.snapshotLocked(value), nil
}

func (s *Store) Snapshots() []Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := orderedConnections(s.values)
	result := make([]Snapshot, 0, len(values))
	for _, value := range values {
		result = append(result, s.snapshotLocked(value))
	}
	return result
}
