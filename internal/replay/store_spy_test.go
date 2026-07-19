package replay

import "context"

// newSpyStore returns a Store that records all calls for assertions.
func newSpyStore() *spyStore {
	return &spyStore{
		records: make(map[workspaceRecordID]Record),
	}
}

type spyStore struct {
	records   map[workspaceRecordID]Record
	calls     []string
	getCalled bool
	getCalls  int
}

func (s *spyStore) Get(ctx context.Context, workspaceSlug string, id ID) (Record, bool, error) {
	_ = ctx
	s.getCalled = true
	s.getCalls++
	s.calls = append(s.calls, "Get")
	r, ok := s.records[workspaceRecordID{workspaceSlug: workspaceSlug, id: id}]
	if !ok {
		return Record{}, false, nil
	}
	return r.Clone(), true, nil
}

func (s *spyStore) Put(ctx context.Context, workspaceSlug string, record Record) error {
	_ = ctx
	s.calls = append(s.calls, "Put")
	s.records[workspaceRecordID{workspaceSlug: workspaceSlug, id: record.ID}] = record.Clone()
	return nil
}
