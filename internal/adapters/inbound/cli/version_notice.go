package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

const installCommand = "curl -fsSL https://swobu.com/install.sh | sh"
const latestVersionURL = "https://api.github.com/repos/swobuforge/swobu/releases/latest"

var fetchLatestVersion = defaultFetchLatestVersion
var latestVersionHTTPClient = &http.Client{Timeout: 500 * time.Millisecond}

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

	currentRaw := strings.TrimSpace(controlplane.SwobuVersion()) // swobu:io-string source=boundary
	latestRaw, err := fetchLatestVersion()
	if err != nil {
		return versionNoticeDecision{}
	}
	latest := sanitizeLatestVersion(latestRaw)
	current := strings.TrimSpace(currentRaw) // swobu:io-string source=boundary
	if latest == "" || current == "" || latest == current {
		return versionNoticeDecision{}
	}
	if patchOnlyVersionChange(current, latest) {
		return versionNoticeDecision{}
	}

	rows := []string{
		"versions: " + nonEmptyOr(currentRaw, "dev") + " → " + latest,
		"update: " + installCommand,
		"hide: export " + platformconfig.EnvSkipVersionNotice + "=1",
	}

	return versionNoticeDecision{
		show: true,
		rows: rows,
	}
}

func nonEmptyOr(value string, fallback string) string {
	trimmed := strings.TrimSpace(value) // swobu:io-string source=boundary
	if trimmed == "" {
		return fallback
	}
	return trimmed
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

func patchOnlyVersionChange(current string, latest string) bool {
	curSemver, okCur := parseSemverLike(current)
	latSemver, okLat := parseSemverLike(latest)
	if !okCur || !okLat {
		return false
	}
	return curSemver.major == latSemver.major && curSemver.minor == latSemver.minor && curSemver.patch != latSemver.patch
}

type semverLikeVersion struct {
	major int
	minor int
	patch int
}

func parseSemverLike(raw string) (semverLikeVersion, bool) {
	value := strings.TrimSpace(raw) // swobu:io-string source=boundary
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return semverLikeVersion{}, false
	}
	main := value
	if cut := strings.IndexAny(main, "-+"); cut >= 0 {
		main = main[:cut]
	}
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semverLikeVersion{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semverLikeVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semverLikeVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semverLikeVersion{}, false
	}
	return semverLikeVersion{major: major, minor: minor, patch: patch}, true
}
