package session

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// newSpyStore returns a Store that records all calls for assertions.
func newSpyStore() *spyStore {
	return &spyStore{
		records: make(map[workspaceRecordID]Checkpoint),
	}
}

type spyStore struct {
	records   map[workspaceRecordID]Checkpoint
	calls     []string
	getCalled bool
	getCalls  int
}

func (s *spyStore) Get(ctx context.Context, workspaceSlug string, id canonical.SwobuResponseID) (Checkpoint, bool, error) {
	_ = ctx
	s.getCalled = true
	s.getCalls++
	s.calls = append(s.calls, "Get")
	r, ok := s.records[workspaceRecordID{workspaceSlug: workspaceSlug, id: id}]
	if !ok {
		return Checkpoint{}, false, nil
	}
	return r.Clone(), true, nil
}

func (s *spyStore) Put(ctx context.Context, workspaceSlug string, record Checkpoint) error {
	_ = ctx
	s.calls = append(s.calls, "Put")
	s.records[workspaceRecordID{workspaceSlug: workspaceSlug, id: record.Response.Response().SwobuID}] = record.Clone()
	return nil
}
