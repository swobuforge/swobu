// Package nodefile serializes edits to the single managed Node YAML file.
// It is a storage adapter for the existing typed routing, Connection, Principal,
// and publication
// codecs, not a resource registry, watcher, or second configuration authority.
package nodefile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
	"gopkg.in/yaml.v3"
)

// File is attached to a configstore lifetime writer lock. A single daemon
// owns this instance and all managed mutations pass through it. This mutex
// serializes disk replacement and the pure, non-reentrant Node admission
// callback on complete candidate bytes. It does not acquire domain locks.
// This keeps independent typed sections from overwriting one another's most
// recent successful commit.
type File struct {
	mu       sync.Mutex
	path     string
	replace  func(string, []byte, os.FileMode) error
	revision string
	readOnly bool
	admit    func([]byte) error
}

var ErrReadOnly = errors.New("config_read_only: externally managed Node YAML cannot be changed")

func Open(path string) (*File, error) { return open(path, false) }

// OpenReadOnly never creates or secures a path or a lock. Serving clients may
// still write their independent execution journals; configuration is immutable.
func OpenReadOnly(path string) (*File, error) { return open(path, true) }

func open(path string, readOnly bool) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if _, err := parseRoot(raw); err != nil {
		return nil, err
	}
	return &File{path: path, replace: atomicfile.Replace, revision: revision(raw), readOnly: readOnly}, nil
}

// InstallAdmission installs one pure whole-Node validation function before the
// daemon accepts any configuration mutation. The callback must be pure and
// must not reenter this File or any domain store. It checks the existing bytes
// first; a previously invalid document cannot be served and then "repaired"
// piecemeal by unrelated writers. Standalone storage tests can use the narrow
// accounting guard without importing application-domain packages here.
func (f *File) InstallAdmission(admit func([]byte) error) error {
	if admit == nil {
		return errors.New("Node admission validator is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return err
	}
	if revision(raw) != f.revision {
		return errors.New("Node configuration changed before installing admission")
	}
	if err := admit(raw); err != nil {
		return fmt.Errorf("invalid Node configuration: %w", err)
	}
	f.admit = admit
	return nil
}

// ConnectionDocument returns the existing Connection codec's document as
// JSON. JSON is confined to the adapter boundary; persisted YAML contains
// ordinary mapping/sequence/scalar nodes that operators can edit.
func (f *File) ConnectionDocument() ([]byte, error) { return f.sectionDocument("connections") }

// PrincipalDocument and PublicationDocument expose detached validated inputs
// to their domain owners, not a second file authority.
func (f *File) PrincipalDocument() ([]byte, error)   { return f.sectionDocument("principals") }
func (f *File) PublicationDocument() ([]byte, error) { return f.sectionDocument("publications") }
func (f *File) AccountingDocument() ([]byte, error)  { return f.sectionDocument("accounting") }

// SectionDocument extracts one owned section from immutable Node bytes. The
// offline validator uses this on ONE captured whole-file snapshot, never on
// several independent reads that can mix revisions during external edits.
func SectionDocument(raw []byte, section string) ([]byte, error) {
	root, err := parseRoot(raw)
	if err != nil {
		return nil, err
	}
	return documentSection(root, section)
}

func (f *File) sectionDocument(section string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	root, err := f.readOwned()
	if err != nil {
		return nil, err
	}
	return documentSection(root, section)
}

func documentSection(root *yaml.Node, section string) ([]byte, error) {
	field := lookup(root, section)
	if field == nil {
		return nil, nil
	}
	if field.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected mapping", section)
	}
	// Keep the accounting policy's stable top-level JSON order. Besides making
	// detached section reads deterministic, this preserves the policy bytes used
	// by revision-bound approval flows while runtime evidence remains separate.
	if section == "accounting" {
		var policy struct {
			SchemaVersion int            `json:"schema_version" yaml:"schema_version"`
			Limits        map[string]any `json:"limits" yaml:"limits"`
			LimitChanges  []any          `json:"limit_changes,omitempty" yaml:"limit_changes,omitempty"`
		}
		if err := field.Decode(&policy); err != nil {
			return nil, fmt.Errorf("%s: %w", section, err)
		}
		raw, err := json.Marshal(policy)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", section, err)
		}
		return raw, nil
	}
	var value any
	if err := field.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s: %w", section, err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", section, err)
	}
	return raw, nil
}

// PublishRouting replaces the routing-owned fields while carrying the latest
// committed Connection document verbatim. It must be called by configstore
// while its routing publication mutex is held, after reference validation.
func (f *File) PublishRouting(routing []byte) error {
	if f.readOnly {
		return ErrReadOnly
	}
	next, err := parseRoot(routing)
	if err != nil {
		return err
	}
	for _, section := range []string{"connections", "principals", "publications", "accounting"} {
		if lookup(next, section) != nil {
			return fmt.Errorf("routing writer cannot replace Node-owned %s", section)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	current, err := f.readOwned()
	if err != nil {
		return err
	}
	for _, section := range []string{"connections", "principals", "publications", "accounting"} {
		if existing := lookup(current, section); existing != nil {
			appendField(next, section, existing)
		}
	}
	if err := validateAccountingReferences(next); err != nil {
		return err
	}
	return f.write(next)
}

// PublishConnections replaces only Connection-owned fields. The caller is
// the Connection store's existing transaction, holding its own mutex until
// disk publication succeeds. Routing edits remain atomically preserved.
func (f *File) PublishConnections(document []byte) error {
	return f.publishJSONSection("connections", document)
}

func (f *File) PublishPrincipals(document []byte) error {
	return f.publishJSONSection("principals", document)
}

func (f *File) PublishPublications(document []byte) error {
	return f.publishJSONSection("publications", document)
}

func (f *File) PublishAccounting(document []byte) error {
	return f.publishJSONSection("accounting", document)
}

// Publish an independently validated typed section. The authoritative bytes
// are replaced only after the domain owner has validated its candidate.
func (f *File) publishJSONSection(section string, document []byte) error {
	if f.readOnly {
		return ErrReadOnly
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(document))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return fmt.Errorf("%s: %w", section, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s: multiple JSON values are unsupported", section)
		}
		return fmt.Errorf("%s: trailing JSON: %w", section, err)
	}
	if value == nil {
		return fmt.Errorf("%s: null document", section)
	}
	node, err := jsonYAML(value)
	if err != nil {
		return err
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: expected object", section)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	root, err := f.readOwned()
	if err != nil {
		return err
	}
	setField(root, section, node)
	if err := validateAccountingReferences(root); err != nil {
		return err
	}
	return f.write(root)
}

// Snapshot returns a detached complete file and an opaque content digest.
// The revision is computed over its committed bytes, not per-resource clocks.
func (f *File) Snapshot() ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return nil, "", err
	}
	if revision(raw) != f.revision {
		return nil, "", errors.New("Node document changed outside the managed snapshot")
	}
	if _, err := parseRoot(raw); err != nil {
		return nil, "", err
	}
	return raw, f.revision, nil
}

func revision(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (f *File) write(root *yaml.Node) error {
	if f.readOnly {
		return ErrReadOnly
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	raw := buf.Bytes()
	if f.admit != nil {
		if err := f.admit(raw); err != nil {
			return fmt.Errorf("Node publication rejected: %w", err)
		}
	}
	if err := f.replace(f.path, raw, 0o600); err != nil {
		return err
	}
	f.revision = revision(raw)
	return nil
}

// readOwned refuses to overwrite an out-of-band modification of a managed
// document. The lifetime path lock prevents another Swobu writer; this check
// also makes unsupported manual edits fail rather than silently merging stale
// in-memory routing/Connection snapshots into a foreign file.
func (f *File) readOwned() (*yaml.Node, error) {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return nil, err
	}
	if revision(raw) != f.revision {
		return nil, errors.New("managed Node configuration changed outside Swobu; stop and restart before editing")
	}
	return parseRoot(raw)
}

func parseRoot(raw []byte) (*yaml.Node, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode Node YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple YAML documents are unsupported")
		}
		return nil, fmt.Errorf("decode Node YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("Node configuration must be a mapping")
	}
	root := document.Content[0]
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if seen[key] {
			return nil, fmt.Errorf("duplicate Node field %q", key)
		}
		seen[key] = true
	}
	return root, nil
}

func lookup(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

func appendField(root *yaml.Node, key string, value *yaml.Node) {
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func setField(root *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = value
			return
		}
	}
	appendField(root, key, value)
}

func jsonYAML(value any) (*yaml.Node, error) {
	switch v := value.(type) {
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		// JSON map encoding already canonicalizes keys. Sorting here gives
		// deterministic diffs across runtimes and map insertion orders.
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child, err := jsonYAML(v[k])
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, child)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, val := range v {
			child, err := jsonYAML(val)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}, nil
	case bool:
		if v {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}, nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(v.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v.String()}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	default:
		return nil, fmt.Errorf("unsupported JSON Connection value %T", value)
	}
}

// validateAccountingReferences protects the cross-section Workspace/limit
// invariant at the *common Node commit lock*, not through separately sampled
// domain stores. It therefore rejects both an orphaned budget and deletion of
// a Workspace that still has a spending policy, including racing commits.
func validateAccountingReferences(root *yaml.Node) error {
	policy := lookup(root, "accounting")
	if policy == nil {
		return nil
	}
	limits := lookup(policy, "limits")
	if limits == nil || limits.Kind != yaml.MappingNode {
		return errors.New("accounting.limits: expected mapping")
	}
	workspaces := lookup(root, "workspaces")
	if workspaces == nil || workspaces.Kind != yaml.MappingNode {
		return errors.New("workspaces: expected mapping")
	}
	for i := 0; i+1 < len(limits.Content); i += 2 {
		workspace := limits.Content[i].Value
		if lookup(workspaces, workspace) == nil {
			return fmt.Errorf("accounting.limits.%s: referenced Workspace is absent; remove its spending policy first", workspace)
		}
	}
	return nil
}
