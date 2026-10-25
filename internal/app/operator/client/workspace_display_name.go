package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/routing"
)

// UpdateWorkspaceDisplayName sends reviewed presentation metadata and exact
// lifetime/revision once. Caller owns confirmation; uncertainty never refreshes
// consent or falls back to slug rename. Empty name explicitly clears metadata.
func (c *Client) UpdateWorkspaceDisplayName(ctx context.Context, slug, name, revision string) (workspaceapi.Workspace, error) {
	parsed, err := routing.ParseWorkspaceSlug(slug)
	if err != nil || parsed.String() != slug {
		return workspaceapi.Workspace{}, errors.New("exact Workspace slug required")
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 || strings.TrimSpace(name) != name {
		return workspaceapi.Workspace{}, errors.New("exact valid Workspace display name required")
	}
	for _, value := range name {
		if unicode.IsControl(value) {
			return workspaceapi.Workspace{}, errors.New("Workspace display name contains control characters")
		}
	}
	if revision == "" || strings.ContainsAny(revision, "\"\\, \t\r\n") {
		return workspaceapi.Workspace{}, errors.New("reviewed Workspace revision required")
	}
	raw, err := json.Marshal(map[string]string{"display_name": name})
	if err != nil {
		return workspaceapi.Workspace{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/api/v1/workspaces/"+url.PathEscape(slug), bytes.NewReader(raw))
	if err != nil {
		return workspaceapi.Workspace{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+revision+`"`)
	req.GetBody = nil
	response, err := c.http.Do(req)
	if err != nil {
		return workspaceapi.Workspace{}, errors.New("Workspace display update not acknowledged; outcome may have committed; inspect current metadata before a new decision")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return workspaceapi.Workspace{}, errorFromResponse(response, "Workspace display update failed")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&fields) != nil || fields == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return workspaceapi.Workspace{}, errors.New("invalid Workspace display acknowledgement; inspect current metadata")
	}
	var actualName *string
	var workspace workspaceapi.Workspace
	encoded, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(fields["display_name"], &actualName) != nil || actualName == nil || *actualName != name || json.Unmarshal(encoded, &workspace) != nil || workspace.Slug != slug || workspace.ManagementRevision == 0 || workspace.ManagementRevision > (1<<53)-1 || workspace.ManagementETag == "" || response.Header.Get("ETag") != `"`+workspace.ManagementETag+`"` {
		return workspaceapi.Workspace{}, errors.New("Workspace display acknowledgement contradicts reviewed identity; inspect current metadata")
	}
	return workspace, nil
}
