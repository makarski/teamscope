package ingest

import (
	"time"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
)

// ProgressOf computes the delivery status and completion ratio of an epic or
// standalone issue, applying status-bucketing rules against the configured
// statuses. The epic's own status is authoritative: if it's already
// done-bucketed in Jira, it's reported as StatusDone with 1.0 progress
// regardless of any child ticket sitting in an unrecognized terminal status
// (e.g. a "Won't Do" subtask under an otherwise closed epic). For issues with
// no child tickets, progress is derived from the issue's own status: done =
// 1.0, todo = 0.0, in-progress = 0.5. An overdue standalone issue (past its
// due date) is reported as StatusOverdue with 0.5 progress.
func ProgressOf(re *RawEpic, sn config.StatusNames, now time.Time) (domain.ProgressStatus, float64) {
	total := len(re.Issues)
	if total == 0 {
		return standaloneProgress(re, sn, now)
	}

	epicStatus := re.Epic.Fields.Status.Name
	if contains(sn.Done, epicStatus) {
		return domain.StatusDone, 1.0
	}

	doneCnt := countIssues(re.Issues, sn.Done)
	ratio := float64(doneCnt) / float64(total)
	status := deriveStatus(re, sn, epicStatus, now)
	return status, ratio
}

// standaloneProgress handles issues with no child tickets.
func standaloneProgress(re *RawEpic, sn config.StatusNames, now time.Time) (domain.ProgressStatus, float64) {
	epicStatus := re.Epic.Fields.Status.Name
	if contains(sn.Done, epicStatus) {
		return domain.StatusDone, 1.0
	}
	if contains(sn.ToDo, epicStatus) {
		return domain.StatusToDo, 0.0
	}
	if isOverdue(re, now) {
		return domain.StatusOverdue, 0.5
	}
	return domain.StatusOngoing, 0.5
}

// deriveStatus decides the status for an epic whose own status isn't
// done-bucketed (that case is handled upstream in ProgressOf).
func deriveStatus(re *RawEpic, sn config.StatusNames, epicStatus string, now time.Time) domain.ProgressStatus {
	if contains(sn.ToDo, epicStatus) {
		return domain.StatusToDo
	}
	if isOverdue(re, now) {
		return domain.StatusOverdue
	}
	return domain.StatusOngoing
}

func isOverdue(re *RawEpic, now time.Time) bool {
	return !re.DueDate.IsZero() && now.After(re.DueDate)
}

func countIssues(issues []jira.Issue, statuses []string) int {
	n := 0
	for _, issue := range issues {
		if contains(statuses, issue.Fields.Status.Name) {
			n++
		}
	}
	return n
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
