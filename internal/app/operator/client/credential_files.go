package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/swobuforge/swobu/internal/app/operator/credentialfiles"
)

func (c *Client) BrowseCredentialFiles(ctx context.Context, path string) (credentialfiles.Listing, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/credential-files?path="+url.QueryEscape(path), nil)
	if err != nil {
		return credentialfiles.Listing{}, fmt.Errorf("operator client: credential files request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return credentialfiles.Listing{}, fmt.Errorf("operator client: credential files are unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return credentialfiles.Listing{}, errorFromResponse(resp, "operator client: credential files browse failed")
	}
	var listing credentialfiles.Listing
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return credentialfiles.Listing{}, fmt.Errorf("operator client: credential files response could not be decoded")
	}
	return listing, nil
}
