package replay

import "context"

// newSpyStore returns a Store that records all calls for assertions.
func newSpyStore() *spyStore {
	return &spyStore{
		records: make(map[scopedID]Record),
	}
}

type spyStore struct {
	records   map[scopedID]Record
	calls     []string
	getCalled bool
}

func (s *spyStore) Get(ctx context.Context, scope Scope, id ID) (Record, bool, error) {
	_ = ctx
	s.getCalled = true
	s.calls = append(s.calls, "Get")
	r, ok := s.records[scopedID{Scope: scope, ID: id}]
	if !ok {
		return Record{}, false, nil
	}
	return r.Clone(), true, nil
}

func (s *spyStore) Put(ctx context.Context, scope Scope, record Record) error {
	_ = ctx
	s.calls = append(s.calls, "Put")
	s.records[scopedID{Scope: scope, ID: record.ID}] = record.Clone()
	return nil
}
