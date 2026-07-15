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
	Path           string
	Content        string
	Mode           fs.FileMode
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
		desc, ok := actionDescriptors[actionSpec.Kind]
		if !ok {
			continue
		}
		summary := strings.TrimSpace(actionSpec.Summary) // swobu:io-string source=boundary
		if summary == "" {
			summary = desc.Summary
		}
		content := actionSpec.Content
		if actionSpec.Kind == ActionKindRun && strings.TrimSpace(content) == "" { // swobu:io-string source=boundary
			content = runActionContentTemplate(spec.Run)
		}
		actions = append(actions, ActionSpec{
			ID:      actionSpec.ID,
			Label:   desc.Label,
			Summary: summary,
			Verb:    desc.Verb,
			Content: content,
		})
	}
	return actions
}

func runActionContentTemplate(run *capabilityRunSpec) string {
	if run == nil {
		return ""
	}
	return renderRunCommandDisplay(run.Binary, run.Args, run.Env)
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
	spec, ok := capabilitySpecByID(clientID)
	if !ok || spec.Run == nil {
		return RunCommandSpec{}, false
	}
	vars := buildTemplateVars(baseURL, spec.Vars)
	if model := strings.TrimSpace(modelID); model != "" { // swobu:io-string source=boundary
		vars["primary_model"] = model
	}
	command := RunCommandSpec{
		ClientID: strings.TrimSpace(clientID), // swobu:io-string source=boundary
		Binary:   renderTemplate(spec.Run.Binary, vars),
		Args:     renderTemplateSlice(spec.Run.Args, vars),
		Env:      renderTemplateMap(spec.Run.Env, vars),
	}
	if spec.Run.Prepare != nil {
		command.Prepare = &RunPrepareFileSpec{
			Path:           renderTemplate(spec.Run.Prepare.Path, vars),
			Content:        runPrepareContent(*spec.Run.Prepare, vars),
			Mode:           spec.Run.Prepare.Mode,
			WriteIfMissing: spec.Run.Prepare.WriteIfMissing,
		}
	}
	return command, true
}

func RenderRunCommand(command RunCommandSpec) string {
	return renderRunCommandDisplay(command.Binary, command.Args, command.Env)
}

func renderRunCommandDisplay(binary string, args []string, env map[string]string) string {
	parts := make([]string, 0, 1+len(args)+len(env))
	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for key := range env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+"="+shellToken(env[key]))
		}
	}
	parts = append(parts, shellToken(binary))
	for _, arg := range args {
		parts = append(parts, shellToken(arg))
	}
	return strings.Join(parts, " ")
}

func capabilitySpecByID(clientID string) (capabilityClientSpec, bool) {
	clientID = strings.TrimSpace(clientID) // swobu:io-string source=boundary
	if clientID == "" {
		return capabilityClientSpec{}, false
	}
	for _, spec := range capabilityCatalog() {
		if strings.TrimSpace(spec.Identity.ID) == clientID { // swobu:io-string source=boundary
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

func runPrepareContent(prepare capabilityRunPrepareSpec, vars TemplateVars) string {
	if strings.TrimSpace(prepare.Content) == "" { // swobu:io-string source=boundary
		return ""
	}
	return renderTemplate(prepare.Content, vars)
}
