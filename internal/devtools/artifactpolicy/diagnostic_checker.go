package artifactpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const wireframeRoot = "swobucli/test/compatibility/surface/tui/testdata/wireframes"
const runtimeCompatibilityRoot = "swobucli/test/compatibility/runtime"
const responsesContinuityRoot = "swobucli/test/fixtures/responses_continuity"
const liveMatrixRecordsRoot = "swobucli/test/fixtures/live_matrix/records"
const legacyLiveMatrixRecordsRoot = "test/fixtures/live_matrix/records"

type Diagnostic struct {
	Filename string
	Message  string
}

// Check verifies default artifact classes are referenced by at least one
// non-artifact repo file.
func Check(root string) ([]Diagnostic, error) {
	return CheckWithClasses(root, DefaultClasses())
}

// CheckWithClasses verifies artifacts in the provided classes are referenced by
// at least one non-artifact repo file.
func CheckWithClasses(root string, classes []Class) ([]Diagnostic, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	artifacts, err := classArtifacts(root, classes)
	if err != nil {
		return nil, err
	}
	diagnostics := make([]Diagnostic, 0, len(artifacts))
	refs, err := collectedArtifactReferences(root, artifacts)
	if err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		if len(refs[artifact.RelPath]) > 0 {
			// still enforce provenance rules below for classes that require it.
		} else {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: artifact.RelPath,
				Message:  "unreferenced " + artifact.Class.Name,
			})
		}
		diagnostics = append(diagnostics, provenanceDiagnostics(root, artifact)...)
	}
	slices.SortFunc(diagnostics, func(a, b Diagnostic) int {
		if a.Filename != b.Filename {
			return strings.Compare(a.Filename, b.Filename)
		}
		return strings.Compare(a.Message, b.Message)
	})
	return diagnostics, nil
}

type artifactRef struct {
	Class    Class
	AbsPath  string
	RelPath  string
	BaseName string
}

func classArtifacts(root string, classes []Class) ([]artifactRef, error) {
	collected := make([]artifactRef, 0, 64)
	for _, class := range classes {
		base := resolveClassBase(root, class.Root)
		if strings.TrimSpace(class.Name) == "" || strings.TrimSpace(class.Root) == "" || len(class.Extensions) == 0 {
			continue
		}
		extensions := normalizeExtensions(class.Extensions)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if _, ok := extensions[strings.ToLower(filepath.Ext(path))]; !ok {
				return nil
			}
			relPath := rel(root, path)
			if contains := strings.TrimSpace(class.PathContains); contains != "" && !strings.Contains(relPath, contains) {
				return nil
			}
			for _, suffix := range class.ExcludeFileSuffix {
				if strings.HasSuffix(relPath, strings.TrimSpace(suffix)) {
					return nil
				}
			}
			collected = append(collected, artifactRef{
				Class:    class,
				AbsPath:  path,
				RelPath:  relPath,
				BaseName: filepath.Base(path),
			})
			return nil
		})
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
	}
	slices.SortFunc(collected, func(a, b artifactRef) int {
		return strings.Compare(a.RelPath, b.RelPath)
	})
	return collected, nil
}

func resolveClassBase(root, classRoot string) string {
	base := filepath.Join(root, filepath.FromSlash(strings.TrimSpace(classRoot)))
	if _, err := os.Stat(base); err == nil {
		return base
	}
	const productPrefix = "swobucli/"
	normalized := filepath.ToSlash(strings.TrimSpace(classRoot))
	if strings.HasPrefix(normalized, productPrefix) {
		legacy := strings.TrimPrefix(normalized, productPrefix)
		legacyBase := filepath.Join(root, filepath.FromSlash(legacy))
		if _, err := os.Stat(legacyBase); err == nil {
			return legacyBase
		}
	}
	return base
}

func normalizeExtensions(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		ext := strings.ToLower(strings.TrimSpace(value))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out[ext] = struct{}{}
	}
	return out
}

func collectedArtifactReferences(root string, artifacts []artifactRef) (map[string]map[string]struct{}, error) {
	artifactSet := map[string]struct{}{}
	nameToTargets := map[string][]string{}
	for _, artifact := range artifacts {
		artifactSet[artifact.AbsPath] = struct{}{}
		nameToTargets[artifact.BaseName] = append(nameToTargets[artifact.BaseName], artifact.RelPath)
	}
	refs := map[string]map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if _, skip := artifactSet[path]; skip {
			return nil
		}
		text, ok := safeText(path)
		if !ok {
			return nil
		}
		source := rel(root, path)
		for name, targets := range nameToTargets {
			if !strings.Contains(text, name) {
				continue
			}
			for _, target := range targets {
				if refs[target] == nil {
					refs[target] = map[string]struct{}{}
				}
				refs[target][source] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

func safeText(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return "", false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	return string(data), true
}

func rel(root, path string) string {
	out, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(out)
}

type artifactProvenance struct {
	SourceRecord string `json:"source_record"`
	SourceField  string `json:"source_field"`
	Extraction   string `json:"extraction"`
}

func provenanceDiagnostics(root string, artifact artifactRef) []Diagnostic {
	if !artifact.Class.RequireProvenance {
		return nil
	}
	sidecarPath := artifact.AbsPath + ".provenance.json"
	sidecarRel := rel(root, sidecarPath)
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "missing provenance sidecar " + sidecarRel,
		}}
	}
	var provenance artifactProvenance
	if err := json.Unmarshal(raw, &provenance); err != nil {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  fmt.Sprintf("invalid provenance sidecar JSON %s", sidecarRel),
		}}
	}
	sourceRecord := strings.TrimSpace(provenance.SourceRecord)
	if sourceRecord == "" {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "provenance sidecar missing source_record",
		}}
	}
	sourceRecordSlash := filepath.ToSlash(sourceRecord)
	if !strings.HasPrefix(sourceRecordSlash, liveMatrixRecordsRoot+"/") &&
		!strings.HasPrefix(sourceRecordSlash, legacyLiveMatrixRecordsRoot+"/") {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "provenance source_record must be under " + liveMatrixRecordsRoot,
		}}
	}
	sourceCandidates := []string{
		filepath.Join(root, filepath.FromSlash(sourceRecord)),
		filepath.Join(root, "swobucli", filepath.FromSlash(sourceRecord)),
	}
	var sourceRaw []byte
	var readErr error
	found := false
	for _, sourceAbs := range sourceCandidates {
		sourceRaw, readErr = os.ReadFile(sourceAbs)
		if readErr == nil {
			found = true
			break
		}
	}
	if !found {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "provenance source_record not readable: " + sourceRecord,
		}}
	}
	sourceValue, err := extractSourceField(sourceRaw, strings.TrimSpace(provenance.SourceField))
	if err != nil {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "provenance source_field error: " + err.Error(),
		}}
	}
	if strings.TrimSpace(provenance.Extraction) != "verbatim" {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "provenance extraction must be verbatim",
		}}
	}
	fixtureRaw, err := os.ReadFile(artifact.AbsPath)
	if err != nil {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "fixture read failed during provenance check",
		}}
	}
	if normalizeVerbatimText(string(fixtureRaw)) != normalizeVerbatimText(sourceValue) {
		return []Diagnostic{{
			Filename: artifact.RelPath,
			Message:  "fixture content does not match verbatim source_record field",
		}}
	}
	return nil
}

func extractSourceField(raw []byte, sourceField string) (string, error) {
	var payload struct {
		Response struct {
			Body string `json:"body"`
		} `json:"response"`
		Client struct {
			Response struct {
				Body string `json:"body"`
			} `json:"response"`
		} `json:"client"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	switch sourceField {
	case "response.body":
		return payload.Response.Body, nil
	case "client.response.body":
		return payload.Client.Response.Body, nil
	default:
		return "", fmt.Errorf("unsupported source_field %q", sourceField)
	}
}

func normalizeVerbatimText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimRight(value, "\n")
}
