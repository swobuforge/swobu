package clientconnect

func planEndpointField(file foreignFile, editor stringDocumentEditor, path keyPath, target Target) (plannedMutation, error) {
	before, exists, err := editor.String(file.raw, path)
	if err != nil {
		return plannedMutation{}, err
	}
	plan := Plan{
		ConfigPath: file.logical, Target: target,
		Changes: semanticChange("endpoint", before, exists, target.WorkspaceURL()),
	}
	if plan.AlreadyConfigured() {
		return plannedMutation{plan: plan}, nil
	}
	next, err := editor.SetString(file.raw, path, target.WorkspaceURL())
	if err != nil {
		return plannedMutation{}, err
	}
	return plannedMutation{plan: plan, apply: func() error { return file.replace(next) }}, nil
}
