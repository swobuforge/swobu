package nodeauthority

// Overview exposes one redacted, committed desired-state snapshot. The
// strong revision is a whole-file digest, deliberately distinct from per-
// resource ETags. Never return raw YAML: it includes credential locators,
// credential digests and private audit/approval evidence.
type Overview struct {
	Revision          string `json:"revision"`
	ConfigurationMode string `json:"configuration_mode"`
	Workspaces        int    `json:"workspace_count"`
	Connections       int    `json:"connection_count"`
	Principals        int    `json:"principal_count"`
	Publications      int    `json:"publication_count"`
	BudgetPolicies    int    `json:"budget_policy_count"`
}
