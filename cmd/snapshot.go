package cmd

import (
	"context"
	"log/slog"

	"github.com/makarski/teamscope/report"
)

// runSnapshot builds and stores a snapshot per team, then optionally posts a
// Slack summary when Slack is configured.
func runSnapshot(ctx context.Context, configPath string) error {
	d, err := newDeps(configPath)
	if err != nil {
		return err
	}
	defer d.close()

	runner, err := d.buildRunner()
	if err != nil {
		return err
	}

	messenger := slackOrNil(d)

	for _, team := range d.cfg.Teams {
		slog.Info("building snapshot", "team", team.Name)

		id, err := runner.Run(ctx, team)
		if err != nil {
			return err
		}
		slog.Info("stored snapshot", "team", team.Name, "id", id)

		if messenger == nil {
			continue
		}
		if err := postTeam(ctx, d, messenger, team.Name); err != nil {
			return err
		}
	}
	return nil
}

func postTeam(ctx context.Context, d *deps, messenger *report.SlackMessenger, team string) error {
	snap, err := d.store.Latest(ctx, team)
	if err != nil {
		return err
	}
	return messenger.Post(report.NewTeamView(snap))
}

// slackOrNil returns a messenger only when Slack is fully configured.
func slackOrNil(d *deps) *report.SlackMessenger {
	s := d.cfg.Slack
	if s == nil || s.Token == "" || s.Channel == "" {
		return nil
	}
	return report.NewSlackMessenger(s.Token, s.Channel)
}
