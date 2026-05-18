package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	uicli "github.com/swobuforge/swobu/internal/terminalui/apps/cli"
)

const installCommand = "curl -fsSL https://swobu.com/install.sh | sh"
const latestVersionURL = "https://raw.githubusercontent.com/swobuforge/swobu/refs/heads/master/VERSION"

var fetchLatestVersion = defaultFetchLatestVersion

type versionNoticeDecision struct {
	show bool
	rows []string
}

func emitVersionNoticeIfConfigured(out io.Writer) versionNoticeDecision {
	decision := evaluateVersionNoticePolicy()
	if !decision.show {
		return decision
	}
	uicli.NewStartupConsolePresenter(out).Emit(uicli.StartupEvent{
		Kind: uicli.StartupEventVersionNotice,
		Text: strings.Join(decision.rows, "\n"),
	})
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
		"current version: " + nonEmptyOr(currentRaw, "dev"),
		"latest version: " + latest,
		"update now: " + installCommand,
		"skip this notice: export " + platformconfig.EnvSkipVersionNotice + "=1",
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
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(latestVersionURL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version fetch status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil // swobu:io-string source=boundary
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
