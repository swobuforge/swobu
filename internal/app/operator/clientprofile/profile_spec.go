package clientprofile

import (
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

// TemplateVars carries string substitutions for profile spec templates.
// Keep template rendering intentionally small and string-substitution only.
type TemplateVars map[string]string

// ProfileSpec is a declarative client profile definition compiled into actions.
type ProfileSpec struct {
	Identity Identity
	Vars     func(baseURL string) TemplateVars
	Actions  []ActionSpec
}

// ActionSpec is a declarative action definition.
type ActionSpec struct {
	ID        string
	Label     string
	Summary   string
	Verb      string
	FocusVerb string
	Content   string
}

type profileSpecAdapter struct{ spec ProfileSpec }

func (p profileSpecAdapter) Identity() Identity {
	return p.spec.Identity
}

func (p profileSpecAdapter) Actions(baseURL string) []Action {
	return compileProfileActions(p.spec, baseURL)
}

func compileProfileActions(spec ProfileSpec, baseURL string) []Action {
	vars := buildTemplateVars(baseURL, spec.Vars)
	actions := make([]Action, 0, len(spec.Actions))
	for _, actionSpec := range spec.Actions {
		actions = append(actions, Action{
			ID:        actionSpec.ID,
			Label:     renderTemplate(actionSpec.Label, vars),
			Summary:   renderTemplate(actionSpec.Summary, vars),
			Verb:      renderTemplate(actionSpec.Verb, vars),
			FocusVerb: renderTemplate(actionSpec.FocusVerb, vars),
			Content:   renderTemplate(actionSpec.Content, vars),
		})
	}
	return actions
}

func buildTemplateVars(baseURL string, varsFn func(baseURL string) TemplateVars) TemplateVars {
	vars := defaultTemplateVars(baseURL)
	if varsFn != nil {
		for key, value := range varsFn(baseURL) {
			vars[key] = value
		}
	}
	// Resolve nested placeholders in variables (for example a custom var that
	// references {{openai_base_url}}).
	for i := 0; i < 2; i++ {
		for key, value := range vars {
			vars[key] = renderTemplate(value, vars)
		}
	}
	return vars
}

func renderTemplate(raw string, vars TemplateVars) string {
	if strings.TrimSpace(raw) == "" || len(vars) == 0 {
		return raw
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	replacements := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		replacements = append(replacements, "{{"+key+"}}", vars[key])
	}
	replacer := strings.NewReplacer(replacements...)
	return replacer.Replace(raw)
}

func defaultTemplateVars(baseURL string) TemplateVars {
	base := strings.TrimSpace(baseURL)
	return TemplateVars{
		"base_url":        base,
		"openai_base_url": openAIBaseURL(base),
		"primary_model":   compatibility.PrimaryTargetSelector,
	}
}
