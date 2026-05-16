package clientprofile

import (
	"io/fs"
	"sort"
	"strings"
)

type RunCommandSpec struct {
	ClientID string
	Binary   string
	Args     []string
	Env      map[string]string
	Prepare  *RunPrepareFileSpec
}

type RunPrepareFileSpec struct {
	Path    string
	Content string
	Mode    fs.FileMode
	// WriteIfMissing preserves explicit per-profile intent when preparing files.
	WriteIfMissing bool
}

func profileSpecFromCapability(spec capabilityClientSpec) ProfileSpec {
	return ProfileSpec{
		Identity: spec.Identity,
		Vars:     spec.Vars,
		Actions:  compileCapabilityActions(spec),
	}
}

func compileCapabilityActions(spec capabilityClientSpec) []ActionSpec {
	actions := make([]ActionSpec, 0, len(spec.Actions))
	for _, actionSpec := range spec.Actions {
		desc := actionDescriptors[actionSpec.Kind]
		summary := strings.TrimSpace(actionSpec.Summary) // trimlowerlint:allow boundary canonicalization
		if summary == "" {
			summary = desc.Summary
		}
		content := actionSpec.Content
		if actionSpec.Kind == ActionKindRun && strings.TrimSpace(content) == "" { // trimlowerlint:allow boundary canonicalization
			content = runActionContentTemplate(spec.Run)
		}
		actions = append(actions, ActionSpec{
			ID:        actionSpec.ID,
			Label:     desc.Label,
			Summary:   summary,
			Verb:      desc.Verb,
			FocusVerb: actionSpec.FocusVerb,
			Content:   content,
		})
	}
	return actions
}

func runActionContentTemplate(run *capabilityRunSpec) string {
	if run == nil {
		return ""
	}
	parts := make([]string, 0, 1+len(run.Args)+len(run.Env))
	if len(run.Env) > 0 {
		keys := make([]string, 0, len(run.Env))
		for key := range run.Env {
			if redactDisplayEnvKey(key) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+"="+shellToken(run.Env[key]))
		}
	}
	parts = append(parts, shellToken(run.Binary))
	for _, arg := range run.Args {
		parts = append(parts, shellToken(arg))
	}
	return strings.Join(parts, " ")
}

func redactDisplayEnvKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "OPENAI_API_KEY") // trimlowerlint:allow boundary canonicalization
}

func shellToken(raw string) string {
	if raw == "" {
		return "''"
	}
	if !strings.ContainsAny(raw, " \t\r\n'\"") {
		return raw
	}
	return "'" + strings.ReplaceAll(raw, "'", `'"'"'`) + "'"
}

func ResolveRunCommand(clientID, baseURL, modelID string) (RunCommandSpec, bool) {
	_ = modelID
	spec, ok := capabilitySpecByID(clientID)
	if !ok || spec.Run == nil {
		return RunCommandSpec{}, false
	}
	vars := buildTemplateVars(baseURL, spec.Vars)
	command := RunCommandSpec{
		ClientID: strings.TrimSpace(clientID), // trimlowerlint:allow boundary canonicalization
		Binary:   renderTemplate(spec.Run.Binary, vars),
		Args:     renderTemplateSlice(spec.Run.Args, vars),
		Env:      renderTemplateMap(spec.Run.Env, vars),
	}
	if spec.Run.Prepare != nil {
		command.Prepare = &RunPrepareFileSpec{
			Path:           renderTemplate(spec.Run.Prepare.Path, vars),
			Content:        runPrepareContent(*spec.Run.Prepare, spec, baseURL),
			Mode:           spec.Run.Prepare.Mode,
			WriteIfMissing: spec.Run.Prepare.WriteIfMissing,
		}
	}
	return command, true
}

func capabilitySpecByID(clientID string) (capabilityClientSpec, bool) {
	clientID = strings.TrimSpace(clientID) // trimlowerlint:allow boundary canonicalization
	if clientID == "" {
		return capabilityClientSpec{}, false
	}
	for _, spec := range capabilityCatalog() {
		if strings.TrimSpace(spec.Identity.ID) == clientID { // trimlowerlint:allow boundary canonicalization
			return spec, true
		}
	}
	return capabilityClientSpec{}, false
}

func renderTemplateSlice(values []string, vars TemplateVars) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, renderTemplate(value, vars))
	}
	return out
}

func renderTemplateMap(values map[string]string, vars TemplateVars) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = renderTemplate(value, vars)
	}
	return out
}

func runPrepareContent(prepare capabilityRunPrepareSpec, spec capabilityClientSpec, baseURL string) string {
	if strings.TrimSpace(prepare.FromActionID) == "" { // trimlowerlint:allow boundary canonicalization
		return ""
	}
	actions := compileProfileActions(profileSpecFromCapability(spec), baseURL)
	for _, action := range actions {
		if strings.TrimSpace(action.ID) == strings.TrimSpace(prepare.FromActionID) { // trimlowerlint:allow boundary canonicalization
			return action.Content
		}
	}
	return ""
}
