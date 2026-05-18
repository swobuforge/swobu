package effect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func httpClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

func httpClientWithTimeout(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		return httpClient()
	}
	return &http.Client{Timeout: timeout}
}

func operatorClient() *operatorclient.Client {
	return operatorclient.New(httpClient(), platformconfig.DefaultDaemonURL())
}

func operatorClientWithTimeout(timeout time.Duration) *operatorclient.Client {
	return operatorclient.New(httpClientWithTimeout(timeout), platformconfig.DefaultDaemonURL())
}

func loadJSON[T any](ctx context.Context, rawURL string) (T, error) {
	return loadJSONValidated[T](ctx, rawURL, nil)
}

func loadJSONWithTimeout[T any](ctx context.Context, rawURL string, timeout time.Duration) (T, error) {
	return loadJSONValidatedWithClient[T](ctx, rawURL, nil, httpClientWithTimeout(timeout))
}

func loadJSONValidated[T any](ctx context.Context, rawURL string, validate func(T) error) (T, error) {
	return loadJSONValidatedWithClient[T](ctx, rawURL, validate, httpClient())
}

func loadJSONValidatedWithClient[T any](ctx context.Context, rawURL string, validate func(T) error, client *http.Client) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return zero, fmt.Errorf("request could not be built")
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return zero, fmt.Errorf("request timed out")
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return zero, fmt.Errorf("request timed out")
		}
		return zero, fmt.Errorf("request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			if parsed, parseErr := neturl.Parse(rawURL); parseErr == nil && strings.HasPrefix(parsed.Path, "/_swobu/model-catalog/probe") {
				return zero, fmt.Errorf("daemon is missing /_swobu/model-catalog/probe (404); restart daemon with current swobu binary")
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
