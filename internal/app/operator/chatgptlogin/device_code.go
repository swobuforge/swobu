package chatgptlogin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type deviceUserCodeResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	UserCodeAlt  string          `json:"usercode"`
	Interval     json.RawMessage `json:"interval"`
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

func (s *LoginService) requestDeviceCode(ctx context.Context) (string, string, time.Duration, error) {
	body, _ := json.Marshal(map[string]string{"client_id": strings.TrimSpace(s.config.ClientID)}) // swobu:io-string source=boundary
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceUserCodeURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", 0, fmt.Errorf("device auth request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent)) // swobu:io-string source=boundary
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("device auth start failed")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("device auth start returned status %d%s", resp.StatusCode, formatRemoteAuthError(raw))
	}
	var out deviceUserCodeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", 0, fmt.Errorf("device auth start response could not be decoded")
	}
	code := strings.TrimSpace(out.UserCode) // swobu:io-string source=boundary
	if code == "" {
		code = strings.TrimSpace(out.UserCodeAlt) // swobu:io-string source=boundary
	}
	if strings.TrimSpace(out.DeviceAuthID) == "" || code == "" { // swobu:io-string source=boundary
		return "", "", 0, fmt.Errorf("device auth start response missing required fields")
	}
	return strings.TrimSpace(out.DeviceAuthID), code, parseDeviceInterval(out.Interval), nil // swobu:io-string source=boundary
}

func (s *LoginService) pollDeviceToken(ctx context.Context, deviceAuthID string, userCode string, interval time.Duration) (string, string, bool, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	body, _ := json.Marshal(map[string]string{
		"device_auth_id": strings.TrimSpace(deviceAuthID), // swobu:io-string source=boundary
		"user_code":      strings.TrimSpace(userCode),     // swobu:io-string source=boundary
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return "", "", false, fmt.Errorf("device auth poll request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", strings.TrimSpace(s.config.UserAgent)) // swobu:io-string source=boundary
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("device auth poll failed")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return "", "", false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", false, fmt.Errorf("device auth poll returned status %d%s", resp.StatusCode, formatRemoteAuthError(raw))
	}
	var out deviceTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", false, fmt.Errorf("device auth poll response could not be decoded")
	}
	return strings.TrimSpace(out.AuthorizationCode), strings.TrimSpace(out.CodeVerifier), true, nil // swobu:io-string source=boundary
}

func parseDeviceInterval(raw json.RawMessage) time.Duration {
	if len(raw) == 0 {
		return 5 * time.Second
	}
	var asInt int
	if err := json.Unmarshal(raw, &asInt); err == nil && asInt > 0 {
		return time.Duration(asInt) * time.Second
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(asString)); convErr == nil && n > 0 { // swobu:io-string source=boundary
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Second
}
