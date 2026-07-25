package producttelemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ReportUploader POSTs a closed ProductReport to the product-telemetry
// endpoint. It accepts only ProductReport — never raw evidence or arbitrary
// attributes (the primary data-exfiltration invariant). Upload is best-effort:
// errors are returned to the caller and must never affect request execution.
// See product-telemetry.md §5, §10.
type reportUploader struct {
	endpointURL string
	client      *http.Client
}

// NewReportUploader targets the given absolute endpoint URL (e.g.
// "https://swobu.com/api/v1/telemetry"). The client sets no Timeout of its own:
// the caller's context (set in flush) owns the single attempt deadline, so the
// two cannot race. One attempt is used; product telemetry is allowed to be lossy.
func newReportUploader(endpointURL string) *reportUploader {
	return &reportUploader{
		endpointURL: endpointURL,
		client:      &http.Client{},
	}
}

// Upload serializes and POSTs the report, returning nil only on a 2xx response.
func (u *reportUploader) Upload(ctx context.Context, report productReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if len(body) > productReportMaxBytes {
		return fmt.Errorf("product report exceeds %d bytes: %d", productReportMaxBytes, len(body))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpointURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build report request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("post report: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("report rejected: status %d", resp.StatusCode)
}
