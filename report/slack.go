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
		contextBlock(fmt.Sprintf("rubric: %s · %s · %d epics", view.Rubric, view.TakenAt, view.EpicCount)),
		section(focusLine(view)),
		section(coverageLine(view.Coverage)),
	}
	if drift := driftSection(view.Drift); drift != nil {
		blocks = append(blocks, slack.NewDividerBlock(), drift)
	}
	if unmapped := unmappedSection(view.Unmapped); unmapped != nil {
		blocks = append(blocks, unmapped)
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

func focusLine(view TeamView) string {
	drift := "no drift"
	if len(view.Drift) > 0 {
		drift = fmt.Sprintf(":red_circle: %d uncovered goals", len(view.Drift))
	}
	return fmt.Sprintf(
		"*Focus*\n:dart: blocker focus *%d%%*   %s   :grey_question: unmapped *%d*",
		view.BlockerFocus, drift, len(view.Unmapped),
	)
}

func coverageLine(coverage []CriterionCoverage) string {
	if len(coverage) == 0 {
		return "*Coverage*\n_No rubric criteria resolved._"
	}
	lines := []string{"*Coverage*"}
	for _, c := range coverage {
		lines = append(lines, fmt.Sprintf(
			"%s *%s* %s — advancing %d/%d (%d%%)",
			statusEmoji(c.Status), c.Key, c.Title, c.Advancing, c.Total, c.Share,
		))
	}
	return strings.Join(lines, "\n")
}

func driftSection(drift []CriterionCoverage) slack.Block {
	if len(drift) == 0 {
		return nil
	}
	lines := []string{"*Drift — open goals nobody is advancing* :warning:"}
	for _, c := range drift {
		lines = append(lines, fmt.Sprintf("• *%s* %s", c.Key, c.Title))
	}
	return section(strings.Join(lines, "\n"))
}

func unmappedSection(unmapped []EpicView) slack.Block {
	if len(unmapped) == 0 {
		return nil
	}
	lines := []string{"*Unmapped epics — work serving no declared goal*"}
	for _, e := range unmapped {
		lines = append(lines, fmt.Sprintf("• *%s* %s", e.Key, e.Summary))
	}
	return section(strings.Join(lines, "\n"))
}

func statusEmoji(status domain.Status) string {
	switch status {
	case domain.CriterionDone:
		return ":white_check_mark:"
	case domain.CriterionOpen:
		return ":large_yellow_circle:"
	default:
		return ":grey_question:"
	}
}
