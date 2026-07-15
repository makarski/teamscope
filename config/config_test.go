package config

import "testing"

func TestIsPlaceholder(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"<jira-api-token>", true},
		{" <slack-bot-token> ", true},
		{"real-token", false},
		{"", false},
		{"#team-progress", false},
		{"<partial", false},
	}
	for _, tt := range tests {
		if got := isPlaceholder(tt.in); got != tt.want {
			t.Errorf("isPlaceholder(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestClearPlaceholders(t *testing.T) {
	cfg := Config{
		Slack:     &Slack{Token: "<slack-bot-token>", Channel: "#real-channel"},
		Anthropic: &Anthropic{Token: "<anthropic-api-key>"},
		GitHub:    &GitHub{Token: "ghp_real"},
	}
	cfg.clearPlaceholders()

	if cfg.Slack.Token != "" {
		t.Errorf("slack token = %q, want empty", cfg.Slack.Token)
	}
	if cfg.Slack.Channel != "#real-channel" {
		t.Errorf("slack channel wrongly cleared: %q", cfg.Slack.Channel)
	}
	if cfg.Anthropic.Token != "" {
		t.Errorf("anthropic token = %q, want empty", cfg.Anthropic.Token)
	}
	if cfg.GitHub.Token != "ghp_real" {
		t.Errorf("github real token wrongly cleared: %q", cfg.GitHub.Token)
	}
}
