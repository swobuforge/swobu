package testpath

import (
	"strings"
	"testing"
)

func TestToken_NormalizesMixedCaseAndPunctuation(t *testing.T) {
	if got, want := Token("Hello World!"), "hello_world"; got != want {
		t.Fatalf("Token(%q)=%q want %q", "Hello World!", got, want)
	}
}

func TestToken_EmptyReturnsUnnamed(t *testing.T) {
	if got, want := Token("   "), "unnamed"; got != want {
		t.Fatalf("Token(%q)=%q want %q", "   ", got, want)
	}
}

func TestTestID_CanonicalForm(t *testing.T) {
	if got, want := TestID("foo", "bar"), "foo__bar"; got != want {
		t.Fatalf("TestID(foo,bar)=%q want %q", got, want)
	}
}

func TestTestIDToken_PreservesCanonicalSeparator(t *testing.T) {
	if got, want := TestIDToken("Foo File__Test Name"), "foo_file__test_name"; got != want {
		t.Fatalf("TestIDToken()=%q want %q", got, want)
	}
}

func TestFileStem_StripsSuffix(t *testing.T) {
	if got, want := FileStem("/a/b/foo_test.go"), "foo"; got != want {
		t.Fatalf("FileStem(%q)=%q want %q", "/a/b/foo_test.go", got, want)
	}
}

func TestFunctionToken_StripsPackagePrefix(t *testing.T) {
	if got, want := FunctionToken("pkg.TestName", 0), "testname"; got != want {
		t.Fatalf("FunctionToken(%q)=%q want %q", "pkg.TestName", got, want)
	}
}

func TestFunctionNameToken_IgnoresLineNumbers(t *testing.T) {
	if got, want := FunctionNameToken("pkg.TestName"), "testname"; got != want {
		t.Fatalf("FunctionNameToken(%q)=%q want %q", "pkg.TestName", got, want)
	}
}

func TestFunctionToken_IncludesLine(t *testing.T) {
	if got, want := FunctionToken("pkg.TestName", 42), "testname_l42"; got != want {
		t.Fatalf("FunctionToken(%q,%d)=%q want %q", "pkg.TestName", 42, got, want)
	}
}

func TestCallerTestID_ExcludesLineNumber(t *testing.T) {
	got, ok := CallerTestID(nil)
	if !ok {
		t.Fatal("CallerTestID did not find test caller")
	}
	if strings.Contains(got, "_l") {
		t.Fatalf("CallerTestID included line-number entropy: %q", got)
	}
}
