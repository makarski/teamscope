package ingest

import (
	"testing"
	"time"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
)

var statusNames = config.StatusNames{
	Done:       []string{"Done", "Closed"},
	InProgress: []string{"In Progress"},
	ToDo:       []string{"To Do", "Backlog"},
}

func issue(status string) jira.Issue {
	return jira.Issue{Fields: &jira.IssueFields{Status: &jira.Status{Name: status}}}
}

func epicWith(epicStatus string, due time.Time, children ...jira.Issue) *RawEpic {
	return &RawEpic{
		Epic:    jira.Issue{Fields: &jira.IssueFields{Status: &jira.Status{Name: epicStatus}}},
		Issues:  children,
		DueDate: due,
	}
}

func TestProgressOf(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	future := now.AddDate(0, 1, 0)
	past := now.AddDate(0, -1, 0)

	tests := []struct {
		name       string
		epic       *RawEpic
		wantStatus domain.ProgressStatus
		wantRatio  float64
	}{
		{
			name:       "all done and epic done",
			epic:       epicWith("Done", future, issue("Done"), issue("Done")),
			wantStatus: domain.StatusDone,
			wantRatio:  1.0,
		},
		{
			name:       "epic in todo",
			epic:       epicWith("Backlog", future, issue("To Do")),
			wantStatus: domain.StatusToDo,
			wantRatio:  0.0,
		},
		{
			name:       "past due and not done",
			epic:       epicWith("In Progress", past, issue("Done"), issue("In Progress")),
			wantStatus: domain.StatusOverdue,
			wantRatio:  0.5,
		},
		{
			name:       "in progress not overdue",
			epic:       epicWith("In Progress", future, issue("Done"), issue("In Progress")),
			wantStatus: domain.StatusOngoing,
			wantRatio:  0.5,
		},
		{
			name:       "no children ongoing",
			epic:       epicWith("In Progress", future),
			wantStatus: domain.StatusOngoing,
			wantRatio:  0.5,
		},
		{
			name:       "standalone done",
			epic:       epicWith("Done", future),
			wantStatus: domain.StatusDone,
			wantRatio:  1.0,
		},
		{
			name:       "standalone todo",
			epic:       epicWith("To Do", future),
			wantStatus: domain.StatusToDo,
			wantRatio:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotRatio := ProgressOf(tt.epic, statusNames, now)
			if gotStatus != tt.wantStatus {
				t.Errorf("status = %q, want %q", gotStatus, tt.wantStatus)
			}
			if gotRatio != tt.wantRatio {
				t.Errorf("ratio = %v, want %v", gotRatio, tt.wantRatio)
			}
		})
	}
}
