package effect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	operatorclient "github.com/metrofun/swobu/internal/app/operator/client"
	platformconfig "github.com/metrofun/swobu/internal/platform/config"
)

func daemonURL() string {
	if value := strings.TrimSpace(os.Getenv("SWOBU_DAEMON_URL")); value != "" {
		return value
	}
	return platformconfig.DefaultDaemonURL()
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

func operatorClient() *operatorclient.Client {
	return operatorclient.New(httpClient(), daemonURL())
}

func loadJSON[T any](ctx context.Context, rawURL string) (T, error) {
	return loadJSONValidated[T](ctx, rawURL, nil)
}

func loadJSONValidated[T any](ctx context.Context, rawURL string, validate func(T) error) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return zero, fmt.Errorf("request could not be built")
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return zero, fmt.Errorf("request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			if parsed, parseErr := neturl.Parse(rawURL); parseErr == nil && strings.HasPrefix(parsed.Path, "/_swobu/model-catalog/preview") {
				return zero, fmt.Errorf("daemon is missing /_swobu/model-catalog/preview (404); restart daemon with current swobu binary")
			}
		}
		return zero, fmt.Errorf("returned status %d", resp.StatusCode)
	}
	var result T
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&result); err != nil {
		return zero, fmt.Errorf("response could not be decoded")
	}
	// Reject trailing bytes to keep daemon->cockpit contracts explicit.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return zero, fmt.Errorf("response could not be decoded")
	}
	if validate != nil {
		if err := validate(result); err != nil {
			return zero, err
		}
	}
	return result, nil
}
