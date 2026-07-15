package report

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/makarski/teamscope/domain"
)

func TestBuildBlocksWithAttention(t *testing.T) {
	view := NewTeamView(snapshot())
	blocks := buildBlocks(view)

	// header + context + mix + alignment + divider + attention = 6
	if len(blocks) != 6 {
		t.Fatalf("block count = %d, want 6", len(blocks))
	}
	if _, ok := blocks[0].(*slack.HeaderBlock); !ok {
		t.Errorf("first block is not a header: %T", blocks[0])
	}
}

func TestBuildBlocksNoAttention(t *testing.T) {
	view := TeamView{
		Team:    "Clean",
		TakenAt: "2026-07-15",
		Mix:     mixSlices(map[domain.WorkType]float64{domain.WorkBusiness: 1}),
	}
	blocks := buildBlocks(view)

	// no divider/attention when nothing is off-track
	if len(blocks) != 4 {
		t.Errorf("block count = %d, want 4", len(blocks))
	}
}

func TestMixLine(t *testing.T) {
	line := mixLine(mixSlices(map[domain.WorkType]float64{
		domain.WorkBusiness: 0.5, domain.WorkChore: 0.3, domain.WorkRnD: 0.2,
	}))
	for _, want := range []string{"business *50%*", "chore *30%*", "rnd *20%*"} {
		if !strings.Contains(line, want) {
			t.Errorf("mix line missing %q: %s", want, line)
		}
	}
}

func TestAttentionReason(t *testing.T) {
	withNote := EpicView{AlignNote: "off goal", Status: domain.StatusOverdue}
	if got := attentionReason(withNote); got != "off goal" {
		t.Errorf("reason = %q, want note", got)
	}
	noNote := EpicView{Status: domain.StatusOverdue}
	if got := attentionReason(noNote); got != "overdue" {
		t.Errorf("reason = %q, want status", got)
	}
}

func TestWorkTypeEmoji(t *testing.T) {
	if workTypeEmoji(domain.WorkBusiness) == workTypeEmoji(domain.WorkChore) {
		t.Error("distinct work types should have distinct emoji")
	}
}
