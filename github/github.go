// Package github fetches contribution signals from GitHub repositories for a
// team. Merged PRs are attributed to Jira epics by matching the epic key
// (e.g. PT-123) in the PR title.
//
// Only PRs are fetched (not commits) to stay within GitHub's search API rate
// limit (30 requests/min for authenticated users). All repos for a team are
// batched into a single search query, so each team costs 1-5 API calls
// depending on PR volume.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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

// Repo identifies a GitHub repository by owner and name.
type Repo struct {
	Owner string
	Name  string
}

// PullRequest is a merged PR with its title for epic-key matching.
type PullRequest struct {
	Number int
	Title  string
}

// keyPattern matches Jira issue keys like MARIO-3730, TTTL-28, AP-123.
var keyPattern = regexp.MustCompile(`[A-Z][A-Z0-9]+-\d+`)

// nameIndex lets PRs be attributed by epic-summary tokens when no Jira key is
// present in the PR title. It maps a lowercased distinctive token to the epic
// key it came from. A token is distinctive when it belongs to exactly one
// epic; tokens shared across epics are dropped to avoid ambiguous matches.
type nameIndex struct {
	tokenToKey map[string]string
}

// stopWords are common tokens that carry no epic-identifying signal.
var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "for": {}, "to": {},
	"of": {}, "in": {}, "on": {}, "with": {}, "by": {}, "at": {}, "from": {},
	"is": {}, "be": {}, "as": {}, "it": {}, "this": {}, "that": {},
}

// buildNameIndex tokenizes each epic summary and keeps only tokens that map
// to exactly one epic key. Short tokens (<4 chars) and stop words are skipped.
func buildNameIndex(epics []EpicRef) *nameIndex {
	counts := make(map[string]int)
	tokenToKey := make(map[string]string)
	for _, e := range epics {
		tokens := tokenizeSummary(e.Summary)
		for i := range tokens {
			counts[tokens[i]]++
			tokenToKey[tokens[i]] = e.Key
		}
	}
	return &nameIndex{tokenToKey: distinctTokens(counts, tokenToKey)}
}

// distinctTokens returns only tokens that appear exactly once, mapping each
// to its epic key.
func distinctTokens(counts map[string]int, tokenToKey map[string]string) map[string]string {
	distinct := make(map[string]string, len(counts))
	for tok, key := range tokenToKey {
		if counts[tok] == 1 {
			distinct[tok] = key
		}
	}
	return distinct
}

// tokenizeSummary splits a summary into lowercased tokens of >=4 chars,
// skipping stop words.
func tokenizeSummary(summary string) []string {
	var out []string
	for _, field := range strings.Fields(strings.ToLower(summary)) {
		tok := strings.Trim(field, ".,;:!?()[]\"'")
		if len(tok) < 4 {
			continue
		}
		if _, skip := stopWords[tok]; skip {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// matchEpic returns the epic key a PR title should be attributed to. Jira key
// match wins; otherwise the first distinctive summary token found in the title
// is used. Returns "" when nothing matches.
func matchEpic(title string, idx *nameIndex) string {
	if keys := keyPattern.FindAllString(title, -1); len(keys) > 0 {
		return keys[0]
	}
	if idx == nil {
		return ""
	}
	for _, tok := range tokenizeSummary(title) {
		if key, ok := idx.tokenToKey[tok]; ok {
			return key
		}
	}
	return ""
}

// FetchActivity fetches merged PRs for the given repositories in the specified
// time window (since). Returns per-repo PR counts.
func (c *Client) FetchActivity(ctx context.Context, repos []string, since time.Time) (map[string]domain.Activity, error) {
	prsByRepo, err := c.fetchPRsBatched(ctx, repos, since)
	if err != nil {
		return nil, err
	}
	return prCountsByRepo(prsByRepo), nil
}

// FetchAttributedActivity fetches merged PRs, then attributes them to Jira
// epic keys. Attribution first matches the epic key (e.g. PT-123) in the PR
// title; PRs with no key in the title fall back to matching distinctive
// tokens from the epic summary. Returns a map of epic key → Activity.
func (c *Client) FetchAttributedActivity(ctx context.Context, repos []string, since time.Time, epics []EpicRef) (map[string]domain.Activity, error) {
	prsByRepo, err := c.fetchPRsBatched(ctx, repos, since)
	if err != nil {
		return nil, err
	}
	return attributeAllPRs(prsByRepo, epics), nil
}

// prCountsByRepo converts PR lists to domain.Activity counts per repo.
func prCountsByRepo(prsByRepo map[string][]PullRequest) map[string]domain.Activity {
	out := make(map[string]domain.Activity, len(prsByRepo))
	for repo, prs := range prsByRepo {
		out[repo] = domain.Activity{PullRequests: len(prs)}
	}
	return out
}

// EpicRef is the minimal epic info needed for PR attribution: the Jira key
// and the human-readable summary. The summary is used as a fallback when no
// epic key appears in a PR title.
type EpicRef struct {
	Key     string
	Summary string
}

// attributeAllPRs attributes PRs from all repos to epic keys.
func attributeAllPRs(prsByRepo map[string][]PullRequest, epics []EpicRef) map[string]domain.Activity {
	attributed := make(map[string]domain.Activity)
	nameIndex := buildNameIndex(epics)
	for _, prs := range prsByRepo {
		attributePRs(attributed, prs, nameIndex)
	}
	return attributed
}

// attributePRs maps PRs to epic keys. First by Jira key in the title; PRs with
// no key fall back to matching distinctive summary tokens.
func attributePRs(attributed map[string]domain.Activity, prs []PullRequest, nameIndex *nameIndex) {
	for _, pr := range prs {
		key := matchEpic(pr.Title, nameIndex)
		if key == "" {
			continue
		}
		a := attributed[key]
		a.PullRequests++
		attributed[key] = a
	}
}

// AggregateActivity sums PR counts across repos.
func AggregateActivity(activities map[string]domain.Activity) domain.Activity {
	var total domain.Activity
	for _, a := range activities {
		total.PullRequests += a.PullRequests
	}
	return total
}

// fetchPRsBatched fetches merged PRs across all repos in a single search query.
// GitHub search supports multiple repo: qualifiers. Returns PRs grouped by
// repo string ("owner/name"). Capped at 500 results to stay within rate limits.
func (c *Client) fetchPRsBatched(ctx context.Context, repos []string, since time.Time) (map[string][]PullRequest, error) {
	parsedRepos := make([]Repo, 0, len(repos))
	for _, repo := range repos {
		r, ok := parseRepo(repo)
		if !ok {
			continue
		}
		parsedRepos = append(parsedRepos, r)
	}
	if len(parsedRepos) == 0 {
		return nil, nil
	}

	var repoFilters []string
	for _, r := range parsedRepos {
		repoFilters = append(repoFilters, fmt.Sprintf("repo:%s/%s", r.Owner, r.Name))
	}
	q := fmt.Sprintf("%s is:pr is:merged merged:>=%s",
		strings.Join(repoFilters, " "), since.Format("2006-01-02"))
	endpoint := fmt.Sprintf("%s/search/issues?q=%s&per_page=100&sort=updated&order=desc",
		c.baseURL, url.QueryEscape(q))

	return c.paginatePRs(ctx, endpoint)
}

// paginatePRs follows the search pagination and collects PRs grouped by repo.
func (c *Client) paginatePRs(ctx context.Context, endpoint string) (map[string][]PullRequest, error) {
	result := make(map[string][]PullRequest)
	page := 1
	total := 0
	for {
		pageURL := fmt.Sprintf("%s&page=%d", endpoint, page)
		var resp struct {
			Items []struct {
				Number        int    `json:"number"`
				Title         string `json:"title"`
				RepositoryURL string `json:"repository_url"`
			} `json:"items"`
			TotalCount int `json:"total_count"`
		}
		next, err := c.getJSONWithPagination(ctx, pageURL, &resp)
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			repoStr := repoFromURL(item.RepositoryURL)
			result[repoStr] = append(result[repoStr], PullRequest{
				Number: item.Number,
				Title:  item.Title,
			})
		}
		total += len(resp.Items)
		if shouldStopPagination(next, total, len(resp.Items)) {
			break
		}
		page++
	}
	return result, nil
}

// shouldStopPagination reports whether the PR search pagination loop should stop.
func shouldStopPagination(next string, total, pageSize int) bool {
	if next == "" {
		return true
	}
	if total >= 500 {
		return true
	}
	return pageSize < 100
}

// repoFromURL extracts "owner/name" from a GitHub repository API URL.
func repoFromURL(repoURL string) string {
	parts := strings.Split(repoURL, "/repos/")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
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
	parts := strings.Split(linkHeader, ",")
	for _, part := range parts {
		if strings.Contains(part, `rel="next"`) {
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start != -1 && end > start {
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
