// naming-law pass over package loading and type usage in a single file.
package rolelint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Diagnostic struct {
	Filename string
	Line     int
	Column   int
	Message  string
}

// analyzer's semantic walk over package loading and role-bearing usage.
// AST scans, and naming-rule evaluation in one pass.
func Check(patterns []string) ([]Diagnostic, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Fset: fset,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedDeps,
		Tests: false,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}

	allPkgs := flattenPackages(pkgs)
	var diagnostics []Diagnostic
	reportedFiles := make(map[string]bool)
	structs := make(map[structKey]structInfo)

	for _, pkg := range allPkgs {
		if pkg == nil || pkg.Types == nil || pkg.TypesInfo == nil || !isRepoPackage(pkg.PkgPath) {
			continue
		}
		for _, pkgErr := range pkg.Errors {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pkgErr.Pos,
				Message:  pkgErr.Msg,
			})
		}
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Package).Filename
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			base := filepath.Base(filename)
			if isWeakGoBasename(base) && !reportedFiles[filename] {
				pos := pkg.Fset.Position(file.Package)
				diagnostics = append(diagnostics, Diagnostic{
					Filename: pos.Filename,
					Line:     pos.Line,
					Column:   pos.Column,
					Message:  fmt.Sprintf("weak file basename %q; rename the file to its dominant concept or behavior", base),
				})
				reportedFiles[filename] = true
			}
			if dominantType, ok := dominantExportedType(pkg.Fset, file); ok && !hasDominantFilenameTokenOverlap(base, dominantType) {
				pos := pkg.Fset.Position(file.Package)
				diagnostics = append(diagnostics, Diagnostic{
					Filename: pos.Filename,
					Line:     pos.Line,
					Column:   pos.Column,
					Message:  fmt.Sprintf("filename %q must share at least one exact token with dominant object %q", base, dominantType),
				})
			}

			// Scan all comments in the file for the forbidden word.
			// Code marked for removal must be deleted, not annotated.
			for _, cg := range file.Comments {
				for _, c := range cg.List {
					// Skip analysistest "// want" directives.
					if strings.HasPrefix(strings.TrimSpace(c.Text), "// want") {
						continue
					}
					text := strings.ToLower(c.Text)
					if strings.Contains(text, "deprecated") {
						pos := pkg.Fset.Position(c.Slash)
						diagnostics = append(diagnostics, Diagnostic{
							Filename: pos.Filename,
							Line:     pos.Line,
							Column:   pos.Column,
							Message:  "use of 'deprecated' in comments is forbidden; remove obsoleted code instead of annotating it",
						})
					}
				}
			}
			for _, decl := range file.Decls {
				switch typed := decl.(type) {
				case *ast.FuncDecl:
					if typed.Name == nil {
						continue
					}
					if lexeme, ok := forbiddenIdentifierLexeme(typed.Name.Name); ok {
						pos := pkg.Fset.Position(typed.Name.Pos())
						diagnostics = append(diagnostics, Diagnostic{
							Filename: pos.Filename,
							Line:     pos.Line,
							Column:   pos.Column,
							Message:  fmt.Sprintf("identifier %q contains forbidden lexeme %q; rename using domain language", typed.Name.Name, lexeme),
						})
					}
				case *ast.GenDecl:
					for _, spec := range typed.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if s.Name != nil {
								if lexeme, ok := forbiddenIdentifierLexeme(s.Name.Name); ok {
									pos := pkg.Fset.Position(s.Name.Pos())
									diagnostics = append(diagnostics, Diagnostic{
										Filename: pos.Filename,
										Line:     pos.Line,
										Column:   pos.Column,
										Message:  fmt.Sprintf("identifier %q contains forbidden lexeme %q; rename using domain language", s.Name.Name, lexeme),
									})
								}
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if n == nil {
									continue
								}
								if lexeme, ok := forbiddenIdentifierLexeme(n.Name); ok {
									pos := pkg.Fset.Position(n.Pos())
									diagnostics = append(diagnostics, Diagnostic{
										Filename: pos.Filename,
										Line:     pos.Line,
										Column:   pos.Column,
										Message:  fmt.Sprintf("identifier %q contains forbidden lexeme %q; rename using domain language", n.Name, lexeme),
									})
								}
							}
						}
					}
				}
			}

			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					def := pkg.TypesInfo.Defs[typeSpec.Name]
					if def == nil {
						continue
					}
					named, ok := def.Type().(*types.Named)
					if !ok {
						continue
					}
					if _, ok := named.Underlying().(*types.Struct); !ok {
						continue
					}
					key := makeStructKey(pkg.PkgPath, typeSpec.Name.Name)
					structs[key] = structInfo{
						Name:          typeSpec.Name.Name,
						Pos:           typeSpec.Name.Pos(),
						Location:      pkg.Fset.Position(typeSpec.Name.Pos()),
						Type:          named,
						Score:         make(map[string]int),
						ExplicitScore: make(map[string]int),
					}
				}
			}
		}
	}

	globalObserveAssignments(allPkgs, structs)
	globalObserveValueSpecs(allPkgs, structs)
	globalObserveReturns(allPkgs, structs)
	globalObserveCalls(allPkgs, structs)
	globalScoreImplicitInterfacesByPackage(allPkgs, structs)

	for _, info := range sortedStructInfos(structs) {
		pos := info.Location
		if slices.Contains(weakConcreteNames, info.Name) {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  fmt.Sprintf("weak concrete struct name %q; rename it to reveal its dominant role", info.Name),
			})
			continue
		}
		for _, msg := range structKindSuffixDiagnostics(info) {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  msg,
			})
		}

		dominant, count, tied := dominantInterface(info.Score)
		if dominant == "" || count == 0 || tied {
			continue
		}
		if !containsNormalized(info.Name, dominant) {
			diagnostics = append(diagnostics, Diagnostic{
				Filename: pos.Filename,
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  fmt.Sprintf("struct %q is used most often as %q; include that interface noun in the concrete name", info.Name, dominant),
			})
		}
	}

	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Filename != diagnostics[j].Filename {
			return diagnostics[i].Filename < diagnostics[j].Filename
		}
		if diagnostics[i].Line != diagnostics[j].Line {
			return diagnostics[i].Line < diagnostics[j].Line
		}
		if diagnostics[i].Column != diagnostics[j].Column {
			return diagnostics[i].Column < diagnostics[j].Column
		}
		return diagnostics[i].Message < diagnostics[j].Message
	})
	return diagnostics, nil
}

func flattenPackages(roots []*packages.Package) []*packages.Package {
	seen := make(map[string]bool)
	var out []*packages.Package
	var walk func(pkg *packages.Package)
	walk = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true
		out = append(out, pkg)
		for _, imported := range pkg.Imports {
			walk(imported)
		}
	}
	for _, pkg := range roots {
		walk(pkg)
	}
	return out
}

func sortedStructInfos(structs map[structKey]structInfo) []structInfo {
	items := make([]structInfo, 0, len(structs))
	for _, info := range structs {
		items = append(items, info)
	}
	sort.Slice(items, func(i, j int) bool {
		ipos := items[i].Location
		jpos := items[j].Location
		if ipos.Filename != jpos.Filename {
			return ipos.Filename < jpos.Filename
		}
		if ipos.Line != jpos.Line {
			return ipos.Line < jpos.Line
		}
		if ipos.Column != jpos.Column {
			return ipos.Column < jpos.Column
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func globalObserveAssignments(pkgs []*packages.Package, structs map[structKey]structInfo) {
	for _, pkg := range pkgs {
		if !isRepoPackage(pkg.PkgPath) {
			continue
		}
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Package).Filename
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				assign, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i := range min(len(assign.Lhs), len(assign.Rhs)) {
					globalCountUsage(pkg.Fset, pkg.TypesInfo, structs, assign.Rhs[i], pkg.TypesInfo.TypeOf(assign.Lhs[i]), true)
				}
				return true
			})
		}
	}
}

func globalObserveValueSpecs(pkgs []*packages.Package, structs map[structKey]structInfo) {
	for _, pkg := range pkgs {
		if !isRepoPackage(pkg.PkgPath) {
			continue
		}
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Package).Filename
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				spec, ok := node.(*ast.ValueSpec)
				if !ok || spec.Type == nil {
					return true
				}
				target := pkg.TypesInfo.TypeOf(spec.Type)
				for _, value := range spec.Values {
					globalCountUsage(pkg.Fset, pkg.TypesInfo, structs, value, target, true)
				}
				return true
			})
		}
	}
}

// composite literals, and typed results to attribute role-bearing usage.
func globalObserveReturns(pkgs []*packages.Package, structs map[structKey]structInfo) {
	for _, pkg := range pkgs {
		if !isRepoPackage(pkg.PkgPath) {
			continue
		}
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Package).Filename
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Type.Results == nil {
					continue
				}
				var resultTypes []types.Type
				for _, field := range fn.Type.Results.List {
					typ := pkg.TypesInfo.TypeOf(field.Type)
					count := len(field.Names)
					if count == 0 {
						count = 1
					}
					for i := 0; i < count; i++ {
						resultTypes = append(resultTypes, typ)
					}
				}
				inspectReturns(fn.Body, func(ret *ast.ReturnStmt) {
					for i := range min(len(ret.Results), len(resultTypes)) {
						globalCountUsage(pkg.Fset, pkg.TypesInfo, structs, ret.Results[i], resultTypes[i], true)
					}
				})
			}
		}
	}
}

func globalObserveCalls(pkgs []*packages.Package, structs map[structKey]structInfo) {
	for _, pkg := range pkgs {
		if !isRepoPackage(pkg.PkgPath) {
			continue
		}
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Package).Filename
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				calleeType := pkg.TypesInfo.TypeOf(call.Fun)
				sig, ok := unwrapSignature(calleeType)
				if !ok {
					return true
				}
				for i, arg := range call.Args {
					paramType, ok := paramType(sig, i)
					if !ok {
						continue
					}
					globalCountUsage(pkg.Fset, pkg.TypesInfo, structs, arg, paramType, true)
				}
				return true
			})
		}
	}
}

func globalCountUsage(fset *token.FileSet, info *types.Info, structs map[structKey]structInfo, value ast.Expr, target types.Type, explicit bool) {
	structType := namedStruct(info.TypeOf(value))
	if structType == nil {
		return
	}
	interfaceName, ok := roleInterfaceName(target)
	if !ok {
		return
	}
	obj := structType.Obj()
	if obj == nil || obj.Pkg() == nil {
		return
	}
	key := makeStructKey(obj.Pkg().Path(), obj.Name())
	entry, ok := structs[key]
	if !ok {
		return
	}
	if !isIgnoredInterface(interfaceName) {
		entry.Score[interfaceName]++
	}
	if explicit {
		entry.ExplicitScore[interfaceName]++
	}
	structs[key] = entry
}

func globalScoreImplicitInterfacesByPackage(pkgs []*packages.Package, structs map[structKey]structInfo) {
	// First pass: count how many structs implement each interface globally
	implCount := make(map[string]int)
	for _, pkg := range pkgs {
		if pkg == nil || pkg.Types == nil || !isRepoPackage(pkg.PkgPath) {
			continue
		}
		candidates := repoInterfacesForPackage(pkg)
		for key, info := range structs {
			if string(key) != pkg.PkgPath+"."+info.Name {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Name == "" || candidate.Type == nil {
					continue
				}
				if implements(info.Type, candidate.Type) {
					implCount[candidate.Name]++
				}
			}
		}
	}
	// Second pass: score weighted by implementor count
	for _, pkg := range pkgs {
		if pkg == nil || pkg.Types == nil || !isRepoPackage(pkg.PkgPath) {
			continue
		}
		candidates := repoInterfacesForPackage(pkg)
		for key, info := range structs {
			if string(key) != pkg.PkgPath+"."+info.Name {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Name == "" || candidate.Type == nil {
					continue
				}
				if implements(info.Type, candidate.Type) {
					if isIgnoredInterface(candidate.Name) {
						continue
					}
					info.Score[candidate.Name] += implCount[candidate.Name]
				}
			}
			structs[key] = info
		}
	}
}

func repoInterfacesForPackage(pkg *packages.Package) []interfaceCandidate {
	seen := make(map[string]bool)
	var out []interfaceCandidate
	addFromScope := func(pkgPath string, scope *types.Scope, includeUnexported bool) {
		if scope == nil {
			return
		}
		for _, name := range scope.Names() {
			if !includeUnexported && !token.IsExported(name) {
				continue
			}
			obj := scope.Lookup(name)
			typeName, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok {
				continue
			}
			iface, ok := named.Underlying().(*types.Interface)
			if !ok || iface.NumMethods() == 0 {
				continue
			}
			key := pkgPath + "." + name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, interfaceCandidate{Name: name, Type: named})
		}
	}
	addFromScope(pkg.PkgPath, pkg.Types.Scope(), true)
	for _, imported := range pkg.Types.Imports() {
		addFromScope(imported.Path(), imported.Scope(), false)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func isRepoPackage(pkgPath string) bool {
	return strings.HasPrefix(pkgPath, "github.com/metrofun/swobu/")
}
