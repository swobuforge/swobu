package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func ScopedErrorAnchor(scope, key string) string {
	scope = strings.TrimSpace(scope) // trimlowerlint:allow boundary canonicalization
	key = strings.TrimSpace(key)     // trimlowerlint:allow boundary canonicalization
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
	return strings.TrimSpace(model.SaveErrors[anchor]) // trimlowerlint:allow boundary canonicalization
}
