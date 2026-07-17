package target_config

import "sync"

var credentialFixtures sync.Map // map[*TargetConfig]*credentialRow

func init() {
	credentialRowForMount = func(w *TargetConfig, autoFocus bool) *credentialRow {
		row := credentialFixture(w)
		row.autoFocus = autoFocus
		return row
	}
}

func credentialFixture(w *TargetConfig) *credentialRow {
	if existing, ok := credentialFixtures.Load(w); ok {
		return existing.(*credentialRow)
	}
	row := newCredentialRow(w, false)
	actual, _ := credentialFixtures.LoadOrStore(w, row)
	return actual.(*credentialRow)
}
