package rolelint

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

var filenameTokenExempt = map[string]struct{}{
	"doc.go":        {},
	"errors.go":     {},
	"types.go":      {},
	"interfaces.go": {},
}

func dominantExportedType(fset *token.FileSet, file *ast.File) (name string, ok bool) {
	if fset == nil || file == nil {
		return "", false
	}
	methodLOCByRecv := make(map[string]int)
	methodCountByRecv := make(map[string]int)
	constructorLOCByType := make(map[string]int)
	for _, decl := range file.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc {
			continue
		}
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvName := receiverTypeName(fn.Recv.List[0].Type)
			if recvName == "" {
				continue
			}
			methodLOCByRecv[recvName] += nodeLOC(fset, fn)
			methodCountByRecv[recvName]++
			continue
		}
		if fn.Name == nil || !fn.Name.IsExported() {
			continue
		}
		if !strings.HasPrefix(fn.Name.Name, "New") {
			continue
		}
		builtType := strings.TrimPrefix(fn.Name.Name, "New")
		if builtType == "" {
			continue
		}
		constructorLOCByType[builtType] += nodeLOC(fset, fn)
	}

	type candidate struct {
		name     string
		score    int
		declLOC  int
		methods  int
		declRank int
	}
	var candidates []candidate
	declRank := 0
	for _, decl := range file.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, isType := spec.(*ast.TypeSpec)
			if !isType || !typeSpec.Name.IsExported() {
				continue
			}
			declLOC := nodeLOC(fset, typeSpec)
			methodLOC := methodLOCByRecv[typeSpec.Name.Name]
			constructorLOC := constructorLOCByType[typeSpec.Name.Name]
			score := declLOC + methodLOC + constructorLOC
			candidates = append(candidates, candidate{
				name:     typeSpec.Name.Name,
				score:    score,
				declLOC:  declLOC,
				methods:  methodCountByRecv[typeSpec.Name.Name],
				declRank: declRank,
			})
			declRank++
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].methods != candidates[j].methods {
			return candidates[i].methods > candidates[j].methods
		}
		if candidates[i].declLOC != candidates[j].declLOC {
			return candidates[i].declLOC > candidates[j].declLOC
		}
		if candidates[i].name != candidates[j].name {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].declRank < candidates[j].declRank
	})
	return candidates[0].name, true
}

func receiverTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		if ident, ok := typed.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

func nodeLOC(fset *token.FileSet, node ast.Node) int {
	if fset == nil || node == nil {
		return 0
	}
	start := fset.Position(node.Pos()).Line
	end := fset.Position(node.End()).Line
	if start <= 0 || end <= 0 {
		return 0
	}
	if end < start {
		return 1
	}
	return (end - start) + 1
}

func hasDominantFilenameTokenOverlap(base string, dominantType string) bool {
	base = strings.TrimSpace(base)
	if base == "" {
		return true
	}
	if _, exempt := filenameTokenExempt[base]; exempt {
		return true
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	fileNameTokens := tokenizeName(stem)
	fileTokens := tokenSet(fileNameTokens)
	typeTokens := tokenizeName(dominantType)
	for _, tok := range typeTokens {
		if _, ok := fileTokens[tok]; ok {
			return true
		}
	}
	// Allow compact overlap for files that encode multi-word stems without separators,
	// e.g. endpointintent.go vs EndpointIntentRepository.
	compactFileStem := strings.Join(fileNameTokens, "")
	compactType := strings.Join(typeTokens, "")
	if compactFileStem != "" && compactType != "" &&
		(strings.Contains(compactType, compactFileStem) || strings.Contains(compactFileStem, compactType)) {
		return true
	}
	return false
}

func tokenSet(tokens []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		out[tok] = struct{}{}
	}
	return out
}

func tokenizeName(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(string(current)))
		current = current[:0]
	}

	runes := []rune(value)
	for i, r := range runes {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			flush()
			continue
		}
		if len(current) == 0 {
			current = append(current, r)
			continue
		}
		prev := current[len(current)-1]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		// Split CamelCase and acronym boundaries deterministically.
		if unicode.IsUpper(r) && (unicode.IsLower(prev) || (unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next))) {
			flush()
		}
		current = append(current, r)
	}
	flush()
	return tokens
}
