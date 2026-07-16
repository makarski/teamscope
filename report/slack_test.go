package report

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestBuildBlocksWithDrift(t *testing.T) {
	view := NewTeamView(snapshot())
	blocks := buildBlocks(view)

	// header + context + focus + coverage + divider + drift + unmapped = 7
	if len(blocks) != 7 {
		t.Fatalf("block count = %d, want 7", len(blocks))
	}
	if _, ok := blocks[0].(*slack.HeaderBlock); !ok {
		t.Errorf("first block is not a header: %T", blocks[0])
	}
}

func TestBuildBlocksClean(t *testing.T) {
	view := TeamView{
		Team:    "Clean",
		Rubric:  "readiness",
		TakenAt: "2026-07-15",
		Coverage: []CriterionCoverage{
			{Key: "billing", Title: "Billing", Status: "done", Advancing: 1, Total: 1, Share: 100},
		},
	}
	blocks := buildBlocks(view)

	// no divider/drift/unmapped when everything is covered
	if len(blocks) != 4 {
		t.Errorf("block count = %d, want 4", len(blocks))
	}
}

func TestCoverageLine(t *testing.T) {
	line := coverageLine([]CriterionCoverage{
		{Key: "billing", Title: "Billing", Status: "open", Advancing: 1, Total: 2, Share: 40},
	})
	for _, want := range []string{"billing", "Billing", "advancing 1/2", "40%"} {
		if !strings.Contains(line, want) {
			t.Errorf("coverage line missing %q: %s", want, line)
		}
	}
}

func TestFocusLine(t *testing.T) {
	view := NewTeamView(snapshot())
	line := focusLine(view)
	for _, want := range []string{"blocker focus *50%*", "uncovered", "unmapped *1*"} {
		if !strings.Contains(line, want) {
			t.Errorf("focus line missing %q: %s", want, line)
		}
	}
}

func TestStatusEmoji(t *testing.T) {
	if statusEmoji("done") == statusEmoji("open") {
		t.Error("distinct statuses should have distinct emoji")
	}
}
