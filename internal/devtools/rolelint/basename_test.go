package rolelint

import (
	"testing"
)

func TestIsWeakGoBasename_ExactMatches(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"types.go", "helpers.go", "support.go", "misc.go",
		"base.go", "common.go", "engine.go", "plan.go",
		"runtime.go", "util.go", "utils.go", "shared.go",
	} {
		if !isWeakGoBasename(name) {
			t.Errorf("isWeakGoBasename(%q) = false, want true", name)
		}
	}
}

func TestIsWeakGoBasename_SuffixedVariants(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"string_helpers.go", "foo_common.go", "bar_runtime.go",
		"baz_engine.go", "qux_support.go", "my_types.go",
		"textutil.go", "str_utils.go", "data_shared.go",
		"strutil.go", "jsonutils.go", "file_util.go",
	} {
		if !isWeakGoBasename(name) {
			t.Errorf("isWeakGoBasename(%q) = false, want true", name)
		}
	}
}

func TestIsWeakGoBasename_StrongNamesPass(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"endpoint.go", "provider_config.go", "handle_request.go",
		"error_mapping.go", "delivery.go", "normalize_path.go",
		"operator_endpoint_store.go", "action_node.go",
		"clipboard.go", "converters.go", "section_keys.go",
		"database.go", "result.go", "pipeline.go",
	} {
		if isWeakGoBasename(name) {
			t.Errorf("isWeakGoBasename(%q) = true, want false", name)
		}
	}
}
