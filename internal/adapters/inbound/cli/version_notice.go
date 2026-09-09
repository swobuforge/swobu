package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"golang.org/x/mod/semver"
)

const latestVersionURL = "https://api.github.com/repos/swobuforge/swobu/releases/latest"

var fetchLatestVersion = defaultFetchLatestVersion
var latestVersionHTTPClient = &http.Client{Timeout: 500 * time.Millisecond}
var currentSwobuVersion = controlplane.SwobuVersion

type versionNoticeDecision struct {
	show bool
	rows []string
}

func emitVersionNoticeIfConfigured(out io.Writer) versionNoticeDecision {
	decision := evaluateVersionNoticePolicy()
	if !decision.show {
		return decision
	}
	writeNoticeBlock(out, "Update Available", decision.rows)
	return decision
}

func evaluateVersionNoticePolicy() versionNoticeDecision {
	if platformconfig.EnvTruthy(os.Getenv(platformconfig.EnvSkipVersionNotice)) {
		return versionNoticeDecision{}
	}

	currentRaw := strings.TrimSpace(currentSwobuVersion()) // swobu:io-string source=boundary
	latestRaw, err := fetchLatestVersion()
	if err != nil {
		return versionNoticeDecision{}
	}
	latest := sanitizeLatestVersion(latestRaw)
	current := strings.TrimSpace(currentRaw) // swobu:io-string source=boundary
	current = normalizeSemver(current)
	latest = normalizeSemver(latest)
	if !semver.IsValid(current) || !semver.IsValid(latest) || semver.Compare(latest, current) <= 0 {
		return versionNoticeDecision{}
	}

	rows := []string{
		"versions: " + current + " → " + latest,
		"update: swobu update",
		"hide: export " + platformconfig.EnvSkipVersionNotice + "=1",
	}

	return versionNoticeDecision{
		show: true,
		rows: rows,
	}
}

func defaultFetchLatestVersion() (string, error) {
	resp, err := latestVersionHTTPClient.Get(latestVersionURL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version fetch status %d", resp.StatusCode)
	}
	var latest struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return "", err
	}
	return strings.TrimSpace(latest.TagName), nil // swobu:io-string source=boundary
}

func sanitizeLatestVersion(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		candidate := strings.TrimSpace(line) // swobu:io-string source=boundary
		if candidate != "" {
			return candidate
		}
	}
	return ""
}
