package report

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"

	"github.com/makarski/teamscope/domain"
)

// SlackMessenger posts team snapshot summaries to Slack.
type SlackMessenger struct {
	client  *slack.Client
	channel string
}

// NewSlackMessenger builds a messenger for the given token and channel.
func NewSlackMessenger(token, channel string) *SlackMessenger {
	return &SlackMessenger{client: slack.New(token), channel: channel}
}

// Post sends a single team's snapshot summary to the configured channel.
func (sm *SlackMessenger) Post(view TeamView) error {
	blocks := buildBlocks(view)
	_, _, err := sm.client.PostMessage(sm.channel, slack.MsgOptionBlocks(blocks...))
	if err != nil {
		return fmt.Errorf("report: post slack message for %q: %w", view.Team, err)
	}
	return nil
}

func buildBlocks(view TeamView) []slack.Block {
	blocks := []slack.Block{
		headerBlock(view),
		contextBlock(fmt.Sprintf("%s · %d epics", view.TakenAt, view.EpicCount)),
		section(mixLine(view.Mix)),
		section(alignmentLine(view.Alignment)),
	}
	if attention := attentionSection(view.OffTrack); attention != nil {
		blocks = append(blocks, slack.NewDividerBlock(), attention)
	}
	return blocks
}

func headerBlock(view TeamView) slack.Block {
	title := fmt.Sprintf("Teamscope: %s", view.Team)
	return slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, title, false, false))
}

func contextBlock(text string) slack.Block {
	return slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, text, false, false))
}

func section(markdown string) slack.Block {
	return slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, markdown, false, false), nil, nil)
}

func mixLine(mix []MixSlice) string {
	parts := make([]string, 0, len(mix))
	for _, s := range mix {
		parts = append(parts, fmt.Sprintf("%s %s *%d%%*", workTypeEmoji(s.WorkType), s.WorkType, s.Percent))
	}
	return "*Work mix*\n" + strings.Join(parts, "   ")
}

func alignmentLine(a AlignmentBreakdown) string {
	return fmt.Sprintf(
		"*Alignment*\n:white_check_mark: aligned *%d*   :large_yellow_circle: partial *%d*   :red_circle: off-track *%d*   :grey_question: unscored *%d*",
		a.Aligned, a.Partial, a.OffTrack, a.Unknown,
	)
}

func attentionSection(offTrack []EpicView) slack.Block {
	if len(offTrack) == 0 {
		return nil
	}
	lines := []string{"*Needs attention* :warning:"}
	for _, e := range offTrack {
		lines = append(lines, fmt.Sprintf("• *%s* %s — %s", e.Key, e.Summary, attentionReason(e)))
	}
	return section(strings.Join(lines, "\n"))
}

func attentionReason(e EpicView) string {
	if e.AlignNote != "" {
		return e.AlignNote
	}
	return string(e.Status)
}

func workTypeEmoji(wt domain.WorkType) string {
	switch wt {
	case domain.WorkBusiness:
		return ":moneybag:"
	case domain.WorkChore:
		return ":broom:"
	case domain.WorkRnD:
		return ":microscope:"
	default:
		return ":memo:"
	}
}
