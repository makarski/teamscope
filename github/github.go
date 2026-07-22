// Package github fetches contribution signals (PRs and commits) from GitHub
// repositories for a team. These are the secondary activity signal that
// enriches the dashboard and PO narrative alongside Jira epics.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makarski/teamscope/domain"
)

// Client fetches GitHub contribution data using the REST API.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient builds a GitHub client. Returns nil if no token is configured,
// letting callers cleanly skip the activity stage.
func NewClient(token string) *Client {
	if token == "" {
		return nil
	}
	return &Client{
		token:   token,
		baseURL: "https://api.github.com",
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RepoActivity holds the contribution counts for a single repository.
type RepoActivity struct {
	Owner   string
	Name    string
	PRs     int
	Commits int
}

// Repo identifies a GitHub repository by owner and name.
type Repo struct {
	Owner string
	Name  string
}

// FetchActivity fetches PR and commit counts for the given repositories in the
// specified time window (since). Returns aggregated activity per repo.
func (c *Client) FetchActivity(ctx context.Context, repos []string, since time.Time) (map[string]domain.Activity, error) {
	if c == nil {
		return nil, nil
	}

	activities := make(map[string]domain.Activity, len(repos))
	for _, repo := range repos {
		r, ok := parseRepo(repo)
		if !ok {
			continue
		}

		prs, err := c.fetchPRCount(ctx, r, since)
		if err != nil {
			return nil, fmt.Errorf("github: fetch PRs for %s: %w", repo, err)
		}

		commits, err := c.fetchCommitCount(ctx, r, since)
		if err != nil {
			return nil, fmt.Errorf("github: fetch commits for %s: %w", repo, err)
		}

		activities[repo] = domain.Activity{
			PullRequests: prs,
			Commits:      commits,
		}
	}
	return activities, nil
}

// AggregateActivity sums PR and commit counts across repos.
func AggregateActivity(activities map[string]domain.Activity) domain.Activity {
	var total domain.Activity
	for _, a := range activities {
		total.PullRequests += a.PullRequests
		total.Commits += a.Commits
	}
	return total
}

// fetchPRCount counts merged PRs in a repo since the given time.
func (c *Client) fetchPRCount(ctx context.Context, r Repo, since time.Time) (int, error) {
	q := fmt.Sprintf("repo:%s/%s is:pr is:merged merged:>=%s",
		r.Owner, r.Name, since.Format("2006-01-02"))
	endpoint := fmt.Sprintf("%s/search/issues?q=%s&per_page=1", c.baseURL, url.QueryEscape(q))

	var result struct {
		TotalCount int `json:"total_count"`
	}
	if err := c.getJSON(ctx, endpoint, &result); err != nil {
		return 0, err
	}
	return result.TotalCount, nil
}

// fetchCommitCount counts commits in a repo's default branch since the given time.
func (c *Client) fetchCommitCount(ctx context.Context, r Repo, since time.Time) (int, error) {
	// GitHub's commits API doesn't return a total count directly, so we
	// paginate. For activity tracking, we cap at 500 to avoid excessive API
	// calls on very active repos.
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits?since=%s&per_page=100",
		c.baseURL, r.Owner, r.Name, since.Format(time.RFC3339))

	count := 0
	page := 1
	for {
		url := fmt.Sprintf("%s&page=%d", endpoint, page)
		var commits []struct {
			SHA string `json:"sha"`
		}
		next, err := c.getJSONWithPagination(ctx, url, &commits)
		if err != nil {
			return 0, err
		}
		count += len(commits)
		if count >= 500 {
			count = 500
			break
		}
		if next == "" {
			break
		}
		if len(commits) < 100 {
			break
		}
		page++
	}
	return count, nil
}

// getJSON makes an authenticated GET request and decodes JSON.
func (c *Client) getJSON(ctx context.Context, endpoint string, v any) error {
	_, err := c.getJSONWithPagination(ctx, endpoint, v)
	return err
}

// getJSONWithPagination makes an authenticated GET request, decodes JSON, and
// returns the "next" URL from the Link header for pagination.
func (c *Client) getJSONWithPagination(ctx context.Context, endpoint string, v any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: status %d for %s", resp.StatusCode, endpoint)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return "", fmt.Errorf("github: decode: %w", err)
	}

	return parseNextLink(resp.Header.Get("Link")), nil
}

// parseNextLink extracts the "next" URL from a GitHub Link header.
func parseNextLink(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}
	// Link: <https://api.github.com/...&page=2>; rel="next", <...>; rel="last"
	parts := strings.Split(linkHeader, ",")
	for _, part := range parts {
		if strings.Contains(part, `rel="next"`) {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start != -1 && end != -1 {
				return part[start+1 : end]
			}
		}
	}
	return ""
}

// parseRepo splits "owner/name" or "https://github.com/owner/name" into a Repo.
func parseRepo(repo string) (Repo, bool) {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimPrefix(repo, "http://github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimSuffix(repo, "/")

	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return Repo{}, false
	}
	return Repo{Owner: parts[0], Name: parts[1]}, true
}
