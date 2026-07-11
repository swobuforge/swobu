package replay

import (
	"github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/exchange"
)

func withRuntimeRunner(runner exchange.Runner) exchange.Runner {
	runner.Runtime = exchangeruntime.NewResolver()
	return runner
}
