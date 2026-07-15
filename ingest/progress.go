package ingest

import (
	"time"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
)

// ProgressOf computes the delivery status and completion ratio of an epic,
// applying roadsnap's status-bucketing rules against the configured statuses.
func ProgressOf(re *RawEpic, sn config.StatusNames, now time.Time) (domain.ProgressStatus, float64) {
	doneCnt := countIssues(re.Issues, sn.Done)
	total := len(re.Issues)

	ratio := 0.0
	if total > 0 {
		ratio = float64(doneCnt) / float64(total)
	}

	status := deriveStatus(re, sn, doneCnt, total, now)
	return status, ratio
}

func deriveStatus(re *RawEpic, sn config.StatusNames, doneCnt, total int, now time.Time) domain.ProgressStatus {
	epicStatus := re.Epic.Fields.Status.Name
	allDone := total > 0 && doneCnt == total

	if contains(sn.Done, epicStatus) && allDone {
		return domain.StatusDone
	}
	if contains(sn.ToDo, epicStatus) {
		return domain.StatusToDo
	}
	if isOverdue(re, now) && !contains(sn.ToDo, epicStatus) {
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
