package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/metrofun/swobu/internal/devtools/livematrix"
)

func main() {
	var (
		casesPath = flag.String("cases", "test/fixtures/live_matrix/scenario_cases.json", "path to live scenario-case matrix json")
		outDir    = flag.String("out", "test/fixtures/live_matrix/records", "output directory for captures")
		timeout   = flag.Duration("timeout", 90*time.Second, "per-scenario-case timeout")
		mode      = flag.String("mode", "swobu_session", "capture mode: swobu_session or direct")
	)
	flag.Parse()

	scenarioCases, err := livematrix.LoadScenarioCases(*casesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load scenario cases: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: *timeout}
	failed := 0
	for _, scenarioCase := range scenarioCases {
		resolved, err := livematrix.ResolveScenarioCase(scenarioCase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scenario_case %s resolve failed: %v\n", scenarioCase.ID, err)
			failed++
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		var (
			capture livematrix.Capture
			capErr  error
		)
		switch *mode {
		case "direct":
			capture, capErr = livematrix.CaptureScenarioCase(ctx, client, resolved)
		case "swobu_session":
			capture, capErr = livematrix.CaptureScenarioCaseViaSwobuTrace(ctx, client, resolved)
		default:
			capErr = fmt.Errorf("unsupported mode %q", *mode)
		}
		cancel()
		if saveErr := livematrix.SaveCapture(*outDir, capture); saveErr != nil {
			fmt.Fprintf(os.Stderr, "scenario_case %s save failed: %v\n", scenarioCase.ID, saveErr)
			failed++
			continue
		}
		if capErr != nil {
			fmt.Fprintf(os.Stderr, "scenario_case %s failed: %v\n", scenarioCase.ID, capErr)
			failed++
			continue
		}
		fmt.Printf("captured %s (%dms)\n", scenarioCase.ID, capture.DurationMS)
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "live matrix completed with %d failures\n", failed)
		os.Exit(1)
	}
	fmt.Println("live matrix capture complete")
}
