package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func ScopedErrorAnchor(scope, key string) string {
	scope = strings.TrimSpace(scope) // swobu:io-string source=boundary
	key = strings.TrimSpace(key)     // swobu:io-string source=boundary
	if scope == "" || key == "" {
		return ""
	}
	return scope + "-row/" + key
}

func ScopedError(model state.Model, scope, key string) string {
	anchor := ScopedErrorAnchor(scope, key)
	if anchor == "" || model.SaveErrors == nil {
		return ""
	}
	return strings.TrimSpace(model.SaveErrors[anchor]) // swobu:io-string source=boundary
}
