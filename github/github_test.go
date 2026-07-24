package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/makarski/teamscope/domain"
)

func TestParseRepo(t *testing.T) {
	tests := []struct {
		input string
		owner string
		name  string
		ok    bool
	}{
		{"owner/repo", "owner", "repo", true},
		{"https://github.com/owner/repo", "owner", "repo", true},
		{"https://github.com/owner/repo.git", "owner", "repo", true},
		{"https://github.com/owner/repo/", "owner", "repo", true},
		{"invalid", "", "", false},
		{"too/many/slashes", "", "", false},
	}
	for _, tt := range tests {
		r, ok := parseRepo(tt.input)
		if r.Owner != tt.owner {
			t.Errorf("parseRepo(%q) owner = %q, want %q", tt.input, r.Owner, tt.owner)
		}
		if r.Name != tt.name {
			t.Errorf("parseRepo(%q) name = %q, want %q", tt.input, r.Name, tt.name)
		}
		if ok != tt.ok {
			t.Errorf("parseRepo(%q) ok = %v, want %v", tt.input, ok, tt.ok)
		}
	}
}

func TestFetchActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/search/issues") {
			w.Write([]byte(`{"total_count": 2, "items": [{"number": 1, "title": "PT-1 Fix bug", "repository_url": "https://api.github.com/repos/owner/repo"}, {"number": 2, "title": "PT-2 Add feature", "repository_url": "https://api.github.com/repos/owner/repo"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		token:   "test-token",
		baseURL: server.URL,
		http:    server.Client(),
	}

	since := time.Now().AddDate(0, 0, -90)
	activities, err := client.FetchActivity(context.Background(), []string{"owner/repo"}, since)
	if err != nil {
		t.Fatal(err)
	}

	a := activities["owner/repo"]
	if a.PullRequests != 2 {
		t.Errorf("PRs = %d, want 2", a.PullRequests)
	}

	testAttributedActivity(t, client, since)
}

func testAttributedActivity(t *testing.T, client *Client, since time.Time) {
	epics := []EpicRef{
		{Key: "PT-1", Summary: "Fix bug"},
		{Key: "PT-2", Summary: "Add feature"},
	}
	attributed, err := client.FetchAttributedActivity(context.Background(), []string{"owner/repo"}, since, epics)
	if err != nil {
		t.Fatal(err)
	}
	if len(attributed) != 2 {
		t.Fatalf("attributed keys = %d, want 2: %v", len(attributed), attributed)
	}
	checkActivity(t, attributed, "PT-1", 1)
	checkActivity(t, attributed, "PT-2", 1)
}

func checkActivity(t *testing.T, m map[string]domain.Activity, key string, prs int) {
	a, ok := m[key]
	if !ok {
		t.Errorf("%s not in attributed map", key)
		return
	}
	if a.PullRequests != prs {
		t.Errorf("%s PRs = %d, want %d", key, a.PullRequests, prs)
	}
}

func TestAggregateActivity(t *testing.T) {
	activities := map[string]domain.Activity{
		"org/repo1": {PullRequests: 3},
		"org/repo2": {PullRequests: 2},
	}
	total := AggregateActivity(activities)
	if total.PullRequests != 5 {
		t.Errorf("total PRs = %d, want 5", total.PullRequests)
	}
}

func TestNewClientNilOnEmptyToken(t *testing.T) {
	if c := NewClient(""); c != nil {
		t.Error("NewClient(\"\") should return nil")
	}
}

func TestAttributeByNameFallback(t *testing.T) {
	// PR with no Jira key should fall back to matching a distinctive summary
	// token. "checkout" appears only in PT-3's summary, so it should attribute
	// to PT-3 even though the PR title has no key.
	epics := []EpicRef{
		{Key: "PT-1", Summary: "Fix login bug"},
		{Key: "PT-2", Summary: "Add search feature"},
		{Key: "PT-3", Summary: "Improve checkout flow"},
	}
	prsByRepo := map[string][]PullRequest{
		"owner/repo": {
			{Number: 10, Title: "Refactor checkout validation"},
			{Number: 11, Title: "PT-1 update auth token"},
			{Number: 12, Title: "Misc cleanup"}, // no match
		},
	}
	attributed := attributeAllPRs(prsByRepo, epics)
	checkActivity(t, attributed, "PT-1", 1) // key match
	checkActivity(t, attributed, "PT-3", 1) // name fallback
	if _, ok := attributed["PT-2"]; ok {
		t.Error("PT-2 should not be attributed: no matching PR")
	}
}

func TestAttributeByNameRejectsAmbiguousTokens(t *testing.T) {
	// "feature" appears in both summaries, so it must not be used for
	// attribution — otherwise a PR titled "feature polish" would be ambiguous.
	epics := []EpicRef{
		{Key: "PT-1", Summary: "Add search feature"},
		{Key: "PT-2", Summary: "Build reporting feature"},
	}
	prsByRepo := map[string][]PullRequest{
		"owner/repo": {
			{Number: 20, Title: "Feature polish"},
		},
	}
	attributed := attributeAllPRs(prsByRepo, epics)
	if len(attributed) != 0 {
		t.Errorf("expected no attribution for ambiguous token, got %v", attributed)
	}
}
