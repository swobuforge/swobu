package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/metrofun/swobu/internal/devtools/executionledger"
)

func main() {
	var tagsArg string
	var pathPrefix string
	var root string
	flag.StringVar(&tagsArg, "tags", "", "comma-separated backlog tags filter (example: P0,TRACKED)")
	flag.StringVar(&pathPrefix, "path-prefix", "", "task path prefix under tasks/ready (example: 06-proof-release)")
	flag.StringVar(&root, "root", ".", "repository root containing tasks/ready")
	flag.Parse()

	filter := executionledger.TaskFilter{
		RequireAnyTag: splitCSV(tagsArg),
		PathPrefix:    strings.TrimSpace(pathPrefix),
	}
	tasks, err := executionledger.LoadBacklogTasks(root, filter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(executionledger.BuildReport(tasks))
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
