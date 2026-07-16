package routes

import "testing"

func TestFormatTarget(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		want     string
	}{
		{"both", "openai", "gpt-4.1", "openai/gpt-4.1"},
		{"empty provider", "", "gpt-4.1", "gpt-4.1"},
		{"empty model", "openai", "", "openai"},
		{"both empty", "", "", ""},
		{"whitespace trimmed", " openai ", " gpt-4.1 ", "openai/gpt-4.1"},
		{"slash in model", "openai", "gpt-4.1-turbo-2024", "openai/gpt-4.1-turbo-2024"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FormatTarget(c.provider, c.model)
			if got != c.want {
				t.Fatalf("FormatTarget(%q, %q) = %q, want %q", c.provider, c.model, got, c.want)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		p, m, err := ParseTarget("openai/gpt-4.1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != "openai" || m != "gpt-4.1" {
			t.Fatalf("ParseTarget = %q, %q; want openai, gpt-4.1", p, m)
		}
	})
	t.Run("extra slashes in model", func(t *testing.T) {
		p, m, err := ParseTarget("openai/gpt-4.1/turbo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != "openai" || m != "gpt-4.1/turbo" {
			t.Fatalf("ParseTarget = %q, %q; want openai, gpt-4.1/turbo", p, m)
		}
	})
	t.Run("whitespace trimmed", func(t *testing.T) {
		p, m, err := ParseTarget("  openai / gpt-4.1  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != "openai" || m != "gpt-4.1" {
			t.Fatalf("ParseTarget = %q, %q; want openai, gpt-4.1", p, m)
		}
	})
	t.Run("empty string", func(t *testing.T) {
		_, _, err := ParseTarget("")
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})
	t.Run("missing slash", func(t *testing.T) {
		_, _, err := ParseTarget("openai")
		if err == nil {
			t.Fatal("expected error for missing slash")
		}
	})
	t.Run("empty provider", func(t *testing.T) {
		_, _, err := ParseTarget("/gpt-4.1")
		if err == nil {
			t.Fatal("expected error for empty provider")
		}
	})
	t.Run("empty model", func(t *testing.T) {
		_, _, err := ParseTarget("openai/")
		if err == nil {
			t.Fatal("expected error for empty model")
		}
	})
}
