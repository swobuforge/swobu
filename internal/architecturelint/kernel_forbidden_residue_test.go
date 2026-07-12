package architecturelint

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestKernelOwnershipNames_RejectForbiddenResidue(t *testing.T) {
	t.Parallel()

	root := fromHere(t, "..")
	scopes := []string{
		filepath.Join(root, "exchange"),
		filepath.Join(root, "carrier"),
		filepath.Join(root, "report"),
		filepath.Join(root, "delivery"),
		filepath.Join(root, "ports"),
	}
	forbiddenWords := []string{
		strings.Join([]string{"com", "pat"}, ""),
		strings.Join([]string{"sh", "im"}, ""),
		strings.Join([]string{"qu", "irk"}, ""),
		strings.Join([]string{"leg", "acy"}, ""),
		strings.Join([]string{"work", "around"}, ""),
	}
	forbidden := regexp.MustCompile(`(?i)(^|_|-)` + strings.Join(forbiddenWords, "|") + `(_|-|$)`)
	for _, scope := range scopes {
		err := filepath.WalkDir(scope, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := strings.ToLower(d.Name()) // swobu:io-string source=domain
				if forbidden.MatchString(name) {
					t.Fatalf("forbidden directory name residue %q in %s", d.Name(), path)
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ".go")) // swobu:io-string source=domain
			if forbidden.MatchString(base) {
				t.Fatalf("forbidden file name residue %q in %s", base, path)
			}

			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
			if parseErr != nil {
				return parseErr
			}
			pkg := strings.ToLower(file.Name.Name) // swobu:io-string source=domain
			if forbidden.MatchString(pkg) {
				t.Fatalf("forbidden package name residue %q in %s", pkg, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scope, err)
		}
	}
}

func TestKernelComments_RejectEscapeHatchPhrase(t *testing.T) {
	t.Parallel()

	root := fromHere(t, "..")
	scopes := []string{
		filepath.Join(root, "exchange"),
		filepath.Join(root, "carrier"),
		filepath.Join(root, "report"),
		filepath.Join(root, "delivery"),
		filepath.Join(root, "ports"),
	}
	for _, scope := range scopes {
		err := filepath.WalkDir(scope, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				return parseErr
			}
			for _, group := range file.Comments {
				for _, comment := range group.List {
					phrase := strings.Join([]string{"escape", " ", "hatch"}, "")
					if strings.Contains(strings.ToLower(comment.Text), phrase) { // swobu:io-string source=domain
						t.Fatalf("forbidden bypass phrase found in %s", path)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", scope, err)
		}
	}
}
