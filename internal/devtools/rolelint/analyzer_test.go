package rolelint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/metrofun/swobu/internal/devtools/rolelint"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, rolelint.Analyzer,
		"rolelinttest/weakname",
		"rolelinttest/deprecated",
		"rolelinttest/forbiddenlexeme",
		"rolelinttest/suffixkind",
		"github.com/metrofun/swobu/internal/rolelintcases/loaderbad",
		"github.com/metrofun/swobu/internal/rolelintcases/loaderok",
	)
}
