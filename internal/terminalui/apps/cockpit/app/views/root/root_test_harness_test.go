package root

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/loop"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	screenassert "github.com/swobuforge/swobu/testscreen/assert"
)

func newTestRuntime(model state.Model) *loop.AppLoop[state.Model] {
	return loop.New(model, state.Reduce)
}

func updateKey(key interaction.Key) interaction.Event {
	return interaction.Event{Kind: interaction.EventKey, Key: key}
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
			screenassert.Text("protocol           converse_stream").Exists(),
		)
	case "bedrock_no_aws_profiles_found":
		must(
			screenassert.Text("provider           AWS Bedrock").Exists(),
			screenassert.Text("profile            no profiles found").Exists(),
			screenassert.Text("select AWS profile before loading models").Exists(),
		)
	case "file_auth_blocked":
		must(
			screenassert.Text("credential         file missing").Exists(),
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
			screenassert.Text("credential         file missing").Exists(),
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
			screenassert.Text("credential        missing").Exists(),
		)
	case "alias_row_closed":
		must(
			screenassert.Text("alias             not set").Exists(),
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
			screenassert.Text("credential        device code").Exists(),
			screenassert.Text("complete browser sign-in to load models").Exists(),
		)
	case "signed_in":
		must(
			screenassert.Text("credential        signed in").Exists(),
			screenassert.Text("sign in another account").Exists(),
			screenassert.Text("model             not selected").Exists(),
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
	for i := 0; i < 20; i++ {
		out := rt.Render(viewport).String()
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, ">") && strings.Contains(line, pattern) {
				return
			}
		}
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	t.Fatalf("could not focus row containing %q; render=%q", pattern, rt.Render(viewport).String())
}

func focusChooserOptionContaining(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, pattern string) {
	t.Helper()
	for i := 0; i < 30; i++ {
		out := rt.Render(viewport).String()
		for _, line := range strings.Split(out, "\n") {
			normalized := strings.ToLower(strings.TrimSpace(line))
			if !strings.Contains(line, ">") || !strings.Contains(line, pattern) {
				continue
			}
			if strings.Contains(normalized, "provider") || strings.Contains(normalized, "credential") {
				continue
			}
			return
		}
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	t.Fatalf("could not focus chooser option containing %q; render=%q", pattern, rt.Render(viewport).String())
}

func openAddModelAndChooseProvider(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, providerName string) {
	t.Helper()
	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "provider")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusChooserOptionContaining(t, rt, viewport, providerName)
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
}

func chooseAddModelAuthOption(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, option string) {
	t.Helper()
	focusRowContaining(t, rt, viewport, "credential")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusChooserOptionContaining(t, rt, viewport, option)
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
}

func selectClientFromChooser(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, label string) {
	t.Helper()
	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "client            ")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, label)
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
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
		return strings.TrimSpace(line[idx+len("path"):])
	}
	return ""
}

func selectAddModelFileCredential(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
	t.Helper()

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyDown))
	rt.Rebuild(Root(), viewport)
	rt.DispatchEvent(updateKey(interaction.KeyDown))
	rt.Rebuild(Root(), viewport)
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusChooserOptionContaining(t, rt, viewport, "file")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	if strings.Contains(rt.Render(viewport).String(), "credential file") {
		return
	}
	t.Fatalf("unable to select file credential option in add-model flow; render=%q", rt.Render(viewport).String())
}

func ensureClientsSectionOpenFromAnyFocusState(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
	t.Helper()
	for i := 0; i < 3; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyEsc))
		rt.Rebuild(Root(), viewport)
	}
	for i := 0; i < 120; i++ {
		out := rt.Render(viewport).String()
		if focusedLineContains(out, "clients") {
			break
		}
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
		if i == 119 {
			t.Fatalf("clients row not reachable; render=%q", out)
		}
	}
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	out := rt.Render(viewport).String()
	if !strings.Contains(out, "clients ▾") {
		t.Fatalf("clients section did not expand; render=%q", out)
	}
}

func focusedLineContains(out string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, line := range strings.Split(strings.ToLower(out), "\n") {
		if !strings.Contains(line, token) {
			continue
		}
		if strings.Contains(line, ">") || strings.Contains(line, "›") {
			return true
		}
	}
	return false
}
