package ingest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pageFetchTimeout bounds a single Confluence page request.
const pageFetchTimeout = 30 * time.Second

// newPageFetcher returns a function that fetches a page's ADF body from the
// Confluence Cloud v2 API using Atlassian basic auth. The body is returned as
// raw JSON for the parser to walk.
func newPageFetcher(baseURL, user, token string) func(string) (json.RawMessage, error) {
	client := &http.Client{Timeout: pageFetchTimeout}
	root := strings.TrimRight(baseURL, "/")

	return func(pageID string) (json.RawMessage, error) {
		endpoint := fmt.Sprintf(
			"%s/wiki/api/v2/pages/%s?body-format=atlas_doc_format",
			root, url.PathEscape(pageID),
		)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("ingest: build page request: %w", err)
		}
		req.SetBasicAuth(user, token)
		req.Header.Set("Accept", "application/json")

		return doPageRequest(client, req)
	}
}

func doPageRequest(client *http.Client, req *http.Request) (json.RawMessage, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ingest: execute page request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ingest: read page response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ingest: page request: status %d: %s", resp.StatusCode, payload)
	}

	return extractADFBody(payload)
}

// extractADFBody pulls body.atlas_doc_format.value (a JSON-encoded ADF string)
// out of the v2 page response and returns it as raw JSON.
func extractADFBody(payload []byte) (json.RawMessage, error) {
	var page struct {
		Body struct {
			ADF struct {
				Value string `json:"value"`
			} `json:"atlas_doc_format"`
		} `json:"body"`
	}
	if err := json.Unmarshal(payload, &page); err != nil {
		return nil, fmt.Errorf("ingest: decode page envelope: %w", err)
	}
	if page.Body.ADF.Value == "" {
		return nil, fmt.Errorf("ingest: page has no atlas_doc_format body")
	}
	return json.RawMessage(page.Body.ADF.Value), nil
}
