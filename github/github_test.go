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
			w.Write([]byte(`{"total_count": 5}`))
			return
		}
		if strings.Contains(r.URL.Path, "/commits") {
			w.Write([]byte(`[{"sha":"abc"},{"sha":"def"},{"sha":"ghi"}]`))
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
	if a.PullRequests != 5 {
		t.Errorf("PRs = %d, want 5", a.PullRequests)
	}
	if a.Commits != 3 {
		t.Errorf("Commits = %d, want 3", a.Commits)
	}
}

func TestAggregateActivity(t *testing.T) {
	activities := map[string]domain.Activity{
		"org/repo1": {PullRequests: 3, Commits: 10},
		"org/repo2": {PullRequests: 2, Commits: 5},
	}
	total := AggregateActivity(activities)
	if total.PullRequests != 5 {
		t.Errorf("total PRs = %d, want 5", total.PullRequests)
	}
	if total.Commits != 15 {
		t.Errorf("total commits = %d, want 15", total.Commits)
	}
}

func TestNewClientNilOnEmptyToken(t *testing.T) {
	if c := NewClient(""); c != nil {
		t.Error("NewClient(\"\") should return nil")
	}
}
