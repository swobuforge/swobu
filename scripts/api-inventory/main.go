// Command api-inventory captures registrations and legacy namespace consumers.
// Its checked baseline prohibits unreviewed additions during domain cutover.
// Reviewed mount-to-dispatch contracts live in api/mounted-route-review.json;
// source conditions are evidence, not an executable reachability model.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type registration struct {
	Source     string              `json:"source"`
	Function   string              `json:"function"`
	Expression string              `json:"expression"`
	Owner      string              `json:"migration_owner"`
	Handler    string              `json:"handler,omitempty"`
	Bindings   map[string][]string `json:"handler_bindings,omitempty"`
}
type consumer struct {
	Source         string `json:"source"`
	Namespace      string `json:"namespace"`
	Owner          string `json:"migration_owner"`
	Occurrences    int    `json:"occurrences"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}
type inventory struct {
	Registrations       []registration      `json:"registrations"`
	DispatchPaths       []registration      `json:"dispatch_paths"`
	LegacyConsumers     []consumer          `json:"legacy_consumers"`
	NamespaceReferences []consumer          `json:"namespace_references"`
	HandlerDispatch     []dispatchCondition `json:"handler_dispatch"`
}

func main() {
	check := flag.Bool("check", false, "check inventory without writing")
	platform := flag.Bool("platform-consumers", false, "include private platform callers in the closeout proof inventory")
	flag.Parse()
	value, err := collect(".")
	if *platform {
		value, err = collectProduct(".")
	}
	if err != nil {
		fail(err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fail(err)
	}
	raw = append(raw, '\n')
	output := "api/migration-inventory.json"
	if *platform {
		output = "../../tasks/pivot-closeout/proof/a01-platform-inventory.json"
	}
	if *check {
		old, err := os.ReadFile(output)
		if err != nil {
			fail(err)
		}
		if !bytes.Equal(old, raw) {
			fail(fmt.Errorf("API migration inventory drift; review route/caller changes and run make api-inventory-generate"))
		}
		return
	}
	if err := os.WriteFile(output, raw, 0644); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

// collectProduct includes the private verification consumers of the public
// runtime. Their source remains owned by the sibling tree; the inventory keeps
// paths relative to OpenCore so migration owners can find every caller.
func collectProduct(root string) (inventory, error) {
	value, err := collect(root)
	if err != nil {
		return value, err
	}
	for _, sibling := range []string{"verification", "tools"} {
		directory := filepath.Join(root, "..", sibling)
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return value, err
		}
		external, err := collect(directory)
		if err != nil {
			return value, err
		}
		for _, reference := range external.NamespaceReferences {
			reference.Source = "../" + sibling + "/" + reference.Source
			value.NamespaceReferences = append(value.NamespaceReferences, reference)
		}
		for _, reference := range external.LegacyConsumers {
			reference.Source = "../" + sibling + "/" + reference.Source
			value.LegacyConsumers = append(value.LegacyConsumers, reference)
		}
	}
	sort.Slice(value.NamespaceReferences, func(i, j int) bool {
		a, b := value.NamespaceReferences[i], value.NamespaceReferences[j]
		return a.Source+a.Namespace < b.Source+b.Namespace
	})
	sort.Slice(value.LegacyConsumers, func(i, j int) bool {
		a, b := value.LegacyConsumers[i], value.LegacyConsumers[j]
		return a.Source+a.Namespace < b.Source+b.Namespace
	})
	return value, nil
}

func collect(root string) (inventory, error) {
	value := inventory{Registrations: []registration{}, DispatchPaths: []registration{}, LegacyConsumers: []consumer{}}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == ".out" || entry.Name() == ".staging" || entry.Name() == "test-results" || path == filepath.Join(root, "api") {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".go" && extension != ".js" && extension != ".ts" && extension != ".tsx" && extension != ".html" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source := filepath.ToSlash(relative)
		if strings.HasPrefix(source, "scripts/api-inventory/") {
			return nil
		}
		for _, namespace := range []string{"/api/v1", "/workspaces/", "/admin/mcp", "/_swobu"} {
			if bytes.Contains(raw, []byte(namespace)) {
				var evidence bytes.Buffer
				for _, line := range bytes.Split(raw, []byte{'\n'}) {
					if bytes.Contains(line, []byte(namespace)) {
						evidence.Write(line)
						evidence.WriteByte('\n')
					}
				}
				owner := "retain"
				legacy := namespace == "/_swobu"
				if legacy {
					owner = consumerOwner(source)
				}
				reference := consumer{Source: source, Namespace: namespace, Owner: owner, Occurrences: bytes.Count(raw, []byte(namespace)), EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(evidence.Bytes()))}
				value.NamespaceReferences = append(value.NamespaceReferences, reference)
				if legacy {
					value.LegacyConsumers = append(value.LegacyConsumers, reference)
				}
			}
		}
		if extension != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileset := token.NewFileSet()
		file, err := parser.ParseFile(fileset, path, raw, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			value.HandlerDispatch = append(value.HandlerDispatch, handlerDispatch(function, source, fileset)...)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				// Manual middleware/handler dispatch is not represented by mux
				// registrations. Retain its path literals as independently checked
				// evidence without claiming they resolve every method branch.
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING && strings.Contains(source, "inbound/httpapi/") {
					path, err := strconv.Unquote(literal.Value)
					if err == nil && (strings.HasPrefix(path, "/api/v1/") || strings.HasPrefix(path, "/_swobu/") || strings.HasPrefix(path, "/workspaces/") || strings.HasPrefix(path, "/workspaces/") || path == "/open" || path == "/operator-bootstrap.js") {
						owner := routeOwner(path)
						value.DispatchPaths = append(value.DispatchPaths, registration{Source: source, Function: function.Name.Name, Expression: path, Owner: owner})
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc" || len(call.Args) == 0 {
					return true
				}
				// Only HTTP mux registration, not slog.Handle or Windows handle APIs.
				if !strings.Contains(source, "inbound/httpapi/") && !strings.Contains(source, "bootstrap/") && !strings.Contains(source, "callback_server.go") {
					return true
				}
				var expression bytes.Buffer
				if err := printer.Fprint(&expression, fileset, call.Args[0]); err != nil {
					return false
				}
				route := expression.String()
				owner := routeOwner(route)
				var handler bytes.Buffer
				if len(call.Args) != 2 {
					return false
				}
				if err := printer.Fprint(&handler, fileset, call.Args[1]); err != nil {
					return false
				}
				value.Registrations = append(value.Registrations, registration{Source: source, Function: function.Name.Name, Expression: route, Owner: owner, Handler: handler.String(), Bindings: handlerBindings(function, call.Args[1], fileset)})
				return true
			})
		}
		return nil
	})
	sort.Slice(value.Registrations, func(i, j int) bool {
		a, b := value.Registrations[i], value.Registrations[j]
		return a.Source+a.Function+a.Expression < b.Source+b.Function+b.Expression
	})
	sort.Slice(value.LegacyConsumers, func(i, j int) bool {
		a, b := value.LegacyConsumers[i], value.LegacyConsumers[j]
		return a.Source+a.Namespace < b.Source+b.Namespace
	})
	sort.Slice(value.HandlerDispatch, func(i, j int) bool {
		a, b := value.HandlerDispatch[i], value.HandlerDispatch[j]
		return a.Source+a.Function+a.Kind+a.Condition+strings.Join(a.Context, "\x00") < b.Source+b.Function+b.Kind+b.Condition+strings.Join(b.Context, "\x00")
	})
	sort.Slice(value.NamespaceReferences, func(i, j int) bool {
		a, b := value.NamespaceReferences[i], value.NamespaceReferences[j]
		return a.Source+a.Namespace < b.Source+b.Namespace
	})
	sort.Slice(value.DispatchPaths, func(i, j int) bool {
		a, b := value.DispatchPaths[i], value.DispatchPaths[j]
		return a.Source+a.Function+a.Expression < b.Source+b.Function+b.Expression
	})
	return value, err
}
func consumerOwner(source string) string {
	switch {
	case strings.Contains(source, "operatorweb/assets/"):
		return "W01"
	case strings.Contains(source, "inbound/cli/"):
		return "L01"
	default:
		return "M01"
	}
}
func routeOwner(route string) string {
	switch {
	case strings.Contains(route, "/permissions"), strings.Contains(route, "/principals/") && strings.Contains(route, "/credentials"), strings.Contains(route, "/access"):
		return "G01"
	case strings.Contains(route, "credentials") || strings.Contains(route, "credential-files") || strings.Contains(route, "target-probe"):
		return "C01"
	case strings.Contains(route, "/agent-sessions"), strings.Contains(route, "/limits/"):
		return "G02"
	case strings.Contains(route, "/clients"), strings.Contains(route, "/client-configurations"):
		return "E02"
	case strings.Contains(route, "/auth-sessions"):
		return "C01"
	case strings.Contains(route, "/down"):
		return "L01"
	case strings.Contains(route, "_swobu"), strings.Contains(route, "prefix"), strings.Contains(route, "/status"), strings.Contains(route, "/tools"), strings.Contains(route, "/search-evidence"), strings.Contains(route, "/audit/events"):
		return "M01"
	default:
		return "retain"
	}
}
