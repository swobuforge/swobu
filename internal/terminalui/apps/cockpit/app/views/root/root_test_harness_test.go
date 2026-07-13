package root

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	apptestharness "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/testharness"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/loop"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/testharness"
	screenassert "github.com/swobuforge/swobu/testscreen/assert"
)

func newTestRuntime(model state.Model) *loop.AppLoop[state.Model] {
	return loop.New(model, state.Reduce)
}

func updateKey(key interaction.Key) interaction.Event {
	return testharness.KeyEvent(key)
}

func assertContainsInOrder(t *testing.T, text string, patterns ...string) {
	t.Helper()
	offset := 0
	for _, pattern := range patterns {
		index := strings.Index(text[offset:], pattern)
		if index < 0 {
			t.Fatalf("render missing %q in order: %q", pattern, text)
		}
		offset += index + len(pattern)
	}
}

func assertCockpitVocabulary(t *testing.T, out string) {
	t.Helper()
	for _, forbidden := range []string{
		"selected target",
		"targets",
		"provider config",
		"credential source",
		"quick launch",
		"\nbehavior\n",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("render contains forbidden cockpit label %q: %q", forbidden, out)
		}
	}
}

func assertRootScenario(t *testing.T, out, scenario string) {
	t.Helper()
	must := func(predicates ...screenassert.Predicate) {
		t.Helper()
		if err := screenassert.EvalNow(out, screenassert.All(predicates...)); err != nil {
			t.Fatalf("scenario %q failed predicate(s): %v\nrender=%q", scenario, err, out)
		}
	}
	mustNot := func(predicates ...screenassert.Predicate) {
		t.Helper()
		for _, predicate := range predicates {
			if err := screenassert.EvalNow(out, screenassert.Not(predicate)); err != nil {
				t.Fatalf("scenario %q matched forbidden predicate %q\nrender=%q", scenario, predicate.String(), out)
			}
		}
	}

	switch scenario {
	case "routing_collapsed_by_esc":
		must(screenassert.Text("routing ▸").Exists(), screenassert.Text("OpenAI · env var · gpt-5.3").Exists())
	case "bedrock_region_derived_from_env":
		must(
			screenassert.Text("provider           AWS Bedrock").Exists(),
			screenassert.Text("region             eu-west-2").Exists(),
			screenassert.Text("protocol           required").Exists(),
		)
	case "bedrock_no_aws_profiles_found":
		must(
			screenassert.Text("provider           AWS Bedrock").Exists(),
			screenassert.Text("profile            auto").Exists(),
			screenassert.Text("select AWS profile before loading models").Exists(),
		)
	case "file_auth_blocked":
		must(
			screenassert.Text("credential         required").Exists(),
			screenassert.Text("credential file    required").Exists(),
			screenassert.Text("set credential file before loading models").Exists(),
		)
	case "env_selected":
		must(
			screenassert.Text("credential         env var").Exists(),
			screenassert.Text("env key            OPENAI_API_KEY").Exists(),
			screenassert.Text("expected           OPENAI_API_KEY").Exists(),
		)
	case "file_selected":
		must(
			screenassert.Text("credential         required").Exists(),
			screenassert.Text("credential file    required").Exists(),
			screenassert.Text("key file           not found").Exists(),
		)
	case "env_reselected":
		must(
			screenassert.Text("credential         env var").Exists(),
			screenassert.Text("env key            OPENAI_API_KEY").Exists(),
			screenassert.Text("expected           OPENAI_API_KEY").Exists(),
		)
	case "add_model_open":
		must(
			screenassert.Text("add model").Exists(),
			screenassert.Text("provider          Provider").Exists(),
			screenassert.Text("credential        required").Exists(),
		)
	case "alias_row_closed":
		must(
			screenassert.Text("alias             auto").Exists(),
			screenassert.Text("add model                                                                          create ↵").Exists(),
		)
		mustNot(screenassert.Text("alias             _").Exists())
	case "alias_row_inline_open":
		must(screenassert.Text("alias             _").Exists(), screenassert.Text("save ↵").Exists())
	case "browser_not_started":
		must(
			screenassert.Text("credential        browser login").Exists(),
			screenassert.Text("open default browser").Exists(),
			screenassert.Text("complete browser sign-in to load models").Exists(),
		)
	case "device_in_progress":
		must(
			screenassert.Text("credential        browser login").Exists(),
			screenassert.Text("complete browser sign-in to load models").Exists(),
		)
	case "signed_in":
		must(
			screenassert.Text("credential        signed in").Exists(),
			screenassert.Text("sign in another account").Exists(),
			screenassert.Text("model             required").Exists(),
		)
	case "models_disclosure_open":
		must(
			screenassert.Text("models             2 configured").Exists(),
			screenassert.Text("model    openai:gpt-5.3:2310255c").Exists(),
			screenassert.Text("model    anthropic:opus:979cec50").Exists(),
		)
	case "models_disclosure_closed_by_esc":
		must(screenassert.Text("models             2 configured").Exists())
		mustNot(screenassert.Text("model    openai:gpt-5.3:2310255c").Exists())
	default:
		t.Fatalf("unknown root scenario assertion %q", scenario)
	}
}

func assertContainsAll(t *testing.T, out string, patterns ...string) {
	t.Helper()
	predicates := make([]screenassert.Predicate, 0, len(patterns))
	for _, pattern := range patterns {
		predicates = append(predicates, screenassert.Text(pattern).Exists())
	}
	if err := screenassert.EvalNow(out, screenassert.All(predicates...)); err != nil {
		t.Fatalf("render missing expected pattern(s): %v\nrender=%q", err, out)
	}
}

func focusRowContaining(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, pattern string) {
	t.Helper()
	testharness.FocusRowContaining(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		pattern,
	)
}

func openRoutingSection(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
	t.Helper()
	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
}

func focusChooserOptionContaining(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, pattern string) {
	t.Helper()
	testharness.FocusChooserOptionContaining(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		pattern,
	)
}

func openAddModelAndChooseProvider(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, providerName string) {
	t.Helper()
	apptestharness.OpenAddModelAndChooseProvider(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyEnter))
			rt.Rebuild(Root(), viewport)
		},
		providerName,
	)
}

func chooseAddModelAuthOption(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, option string) {
	t.Helper()
	apptestharness.ChooseAddModelAuthOption(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyEnter))
			rt.Rebuild(Root(), viewport)
		},
		option,
	)
}

func selectClientFromChooser(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, label string) {
	t.Helper()
	apptestharness.SelectClientFromChooser(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyEnter))
			rt.Rebuild(Root(), viewport)
		},
		label,
	)
}

func currentCredentialPickerPath(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "path") {
			continue
		}
		idx := strings.Index(line, "path")
		if idx < 0 {
			continue
		}
		return strings.TrimSpace(line[idx+len("path"):]) // swobu:io-string source=domain
	}
	return ""
}

func selectAddModelFileCredential(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
	t.Helper()
	apptestharness.SelectAddModelFileCredential(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyEnter))
			rt.Rebuild(Root(), viewport)
		},
	)
}

func ensureClientsSectionOpenFromAnyFocusState(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
	t.Helper()
	apptestharness.EnsureSectionOpenFromAnyFocusState(
		t,
		func() string { return rt.Render(viewport).String() },
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyDown))
			rt.Rebuild(Root(), viewport)
		},
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyEnter))
			rt.Rebuild(Root(), viewport)
		},
		func() {
			rt.DispatchEvent(updateKey(interaction.KeyEsc))
			rt.Rebuild(Root(), viewport)
		},
		"clients",
		"clients ▾",
	)
}
