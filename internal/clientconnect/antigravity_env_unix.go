//go:build !windows

package clientconnect

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func inspectAntigravityEnvironment(s *Service, target Target) (antigravityEnvironmentPlan, error) {
	home, err := s.homeDir()
	if err != nil {
		return antigravityEnvironmentPlan{}, err
	}
	shell := ""
	if s.getenv != nil {
		shell = filepath.Base(strings.TrimSpace(s.getenv("SHELL")))
	}
	var profileName string
	switch shell {
	case "bash":
		profileName = ".bashrc"
	case "zsh":
		profileName = ".zshrc"
	default:
		return antigravityEnvironmentPlan{}, fmt.Errorf("automatic Antigravity configuration supports bash and zsh; persist GEMINI_API_KEY and GOOGLE_GEMINI_BASE_URL manually for shell %q", shell)
	}
	profile, err := inspectForeignFile(filepath.Join(home, profileName), nil)
	if err != nil {
		return antigravityEnvironmentPlan{}, err
	}
	startLines := standaloneLineSpans(profile.raw, antigravityProfileStart)
	endLines := standaloneLineSpans(profile.raw, antigravityProfileEnd)
	if len(startLines) > 1 || len(endLines) > 1 || len(startLines) != len(endLines) {
		return antigravityEnvironmentPlan{}, fmt.Errorf("Antigravity profile contains malformed or duplicated Swobu markers")
	}
	desiredBlock := antigravityProfileBlock(target.WorkspaceURL())
	foreign := append([]byte(nil), profile.raw...)
	currentEndpoint := ""
	endpointExists := false
	fallbackExists := false
	// A semantically correct block is still incomplete when later shell source
	// can override it. Moving the owned block last gives the binding precedence.
	precedenceFinal := true
	if len(startLines) == 1 {
		start := startLines[0].start
		end := endLines[0].end
		if endLines[0].start < start {
			return antigravityEnvironmentPlan{}, fmt.Errorf("Antigravity profile contains malformed Swobu markers")
		}
		block := string(profile.raw[start:end])
		fallbackExists = strings.Contains(block, `export GEMINI_API_KEY="${GEMINI_API_KEY:-swobu-local}"`)
		const endpointPrefix = "export GOOGLE_GEMINI_BASE_URL='"
		if at := strings.Index(block, endpointPrefix); at >= 0 {
			rest := block[at+len(endpointPrefix):]
			if close := strings.IndexByte(rest, '\''); close >= 0 {
				currentEndpoint, endpointExists = rest[:close], true
			}
		}
		precedenceFinal = end == len(profile.raw)
		foreign = append(append([]byte(nil), profile.raw[:start]...), profile.raw[end:]...)
	} else {
		if s.getenv != nil {
			currentEndpoint = strings.TrimSpace(s.getenv("GOOGLE_GEMINI_BASE_URL"))
			endpointExists = currentEndpoint != ""
		}
	}
	changes := semanticChange("endpoint", currentEndpoint, endpointExists, target.WorkspaceURL())
	if !fallbackExists {
		changes = append(changes, Change{Field: "API key fallback", After: "swobu-local"})
	}
	if !precedenceFinal {
		changes = append(changes, Change{Field: "profile precedence", Before: "non-final", BeforeExists: true, After: "final"})
	}
	next := appendAntigravityProfileBlock(foreign, desiredBlock, len(startLines) == 0)
	if !bytes.Equal(profile.raw, next) && len(changes) == 0 {
		changes = append(changes, Change{Field: "profile block", After: "managed"})
	}
	plan := antigravityEnvironmentPlan{configPaths: []string{profile.logical}, changes: changes}
	if !bytes.Equal(profile.raw, next) {
		plan.apply = func(context.Context) error { return profile.replace(next) }
	}
	return plan, nil
}

type byteSpan struct{ start, end int }

// standaloneLineSpans deliberately recognizes only whole marker lines. Marker
// text inside a command or quoted string is unrelated user shell source.
func standaloneLineSpans(raw []byte, marker string) []byteSpan {
	var spans []byteSpan
	for start := 0; start < len(raw); {
		end := bytes.IndexByte(raw[start:], '\n')
		if end < 0 {
			end = len(raw)
		} else {
			end += start
		}
		if string(raw[start:end]) == marker {
			lineEnd := end
			if lineEnd < len(raw) {
				lineEnd++
			}
			spans = append(spans, byteSpan{start: start, end: lineEnd})
		}
		if end == len(raw) {
			break
		}
		start = end + 1
	}
	return spans
}

func appendAntigravityProfileBlock(foreign, block []byte, absent bool) []byte {
	next := append([]byte(nil), foreign...)
	if len(next) != 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	// A new block gets one visual separator. Relocation reuses the exact bytes
	// left by the removed block so repeated inspection reaches a fixed point.
	if absent && len(next) != 0 {
		next = append(next, '\n')
	}
	return append(next, block...)
}
