package clientprofile

import "strings"

// Action is one operator-visible row for a selected client profile.
type Action struct {
	ID        string
	Label     string
	Summary   string
	Verb      string
	FocusVerb string
	Content   string
}

func (a Action) IsConfigured() bool {
	return a.RowLabel() != "" && a.ActionVerb() != ""
}

func (a Action) EffectiveFocusVerb() string {
	if strings.TrimSpace(a.FocusVerb) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(a.FocusVerb) // swobu:io-string source=boundary
	}
	return a.ActionVerb()
}

func (a Action) HasPayload() bool {
	return strings.TrimSpace(a.Content) != "" // swobu:io-string source=boundary
}

func (a Action) IsRunAction() bool {
	return a.ActionVerb() == "run"
}

func (a Action) IsCopyAction() bool {
	return a.ActionVerb() == "copy"
}

func (a Action) RowLabel() string {
	return strings.TrimSpace(a.Label) // swobu:io-string source=boundary
}

func (a Action) ActionSummary() string {
	return strings.TrimSpace(a.Summary) // swobu:io-string source=boundary
}

func (a Action) ActionVerb() string {
	return strings.TrimSpace(a.Verb) // swobu:io-string source=boundary
}
