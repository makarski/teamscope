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
		Bedrock:   &Bedrock{Model: "<bedrock-model-id>", Region: "us-east-1"},
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
	if cfg.Bedrock.Model != "" {
		t.Errorf("bedrock model = %q, want empty", cfg.Bedrock.Model)
	}
	if cfg.Bedrock.Region != "us-east-1" {
		t.Errorf("bedrock region wrongly cleared: %q", cfg.Bedrock.Region)
	}
	if cfg.GitHub.Token != "ghp_real" {
		t.Errorf("github real token wrongly cleared: %q", cfg.GitHub.Token)
	}
}

func baseConfig() Config {
	return Config{
		Jira:  &Jira{BaseURL: "https://x.atlassian.net"},
		Store: &Store{Path: "x.db"},
		Rubrics: []Rubric{
			{Name: "work", Source: "static", Criteria: []Criterion{{Key: "business", Title: "Business"}}},
		},
		Teams: []Team{
			{Name: "Payments", JiraProjects: []string{"PT"}, Rubric: "work"},
		},
	}
}

func TestValidateAcceptsValidRubricRef(t *testing.T) {
	cfg := baseConfig()
	if err := cfg.validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestValidateRejectsUnknownRubric(t *testing.T) {
	cfg := baseConfig()
	cfg.Teams[0].Rubric = "missing"
	if err := cfg.validate(); err == nil {
		t.Error("expected error for unknown rubric reference")
	}
}

func TestValidateRejectsMissingRubric(t *testing.T) {
	cfg := baseConfig()
	cfg.Teams[0].Rubric = ""
	if err := cfg.validate(); err == nil {
		t.Error("expected error for missing team rubric")
	}
}

func TestRubricByName(t *testing.T) {
	cfg := baseConfig()
	if _, ok := cfg.RubricByName("work"); !ok {
		t.Error("work rubric should be found")
	}
	if _, ok := cfg.RubricByName("nope"); ok {
		t.Error("nope rubric should not be found")
	}
}

func TestValidateRejectsMissingRubrics(t *testing.T) {
	cfg := baseConfig()
	cfg.Rubrics = nil
	if err := cfg.validate(); err == nil {
		t.Error("expected error when no rubrics are defined")
	}
}

func TestGoalsHashStableAndSensitive(t *testing.T) {
	cfg := baseConfig()
	h1 := cfg.GoalsHash()
	if h1 != cfg.GoalsHash() {
		t.Error("hash should be stable across calls")
	}
	cfg.Rubrics[0].Criteria[0].Title = "Changed"
	if cfg.GoalsHash() == h1 {
		t.Error("hash should change when a criterion changes")
	}
}

func TestGoalsHashSensitiveToScoringInputs(t *testing.T) {
	baseCfg := baseConfig()
	base := baseCfg.GoalsHash()

	withStatus := baseConfig()
	withStatus.Rubrics[0].Criteria[0].Status = "done"
	if withStatus.GoalsHash() == base {
		t.Error("hash should change when criterion status changes")
	}

	withHint := baseConfig()
	withHint.Rubrics[0].KeywordHints = []KeywordHint{{Keyword: "billing", Criterion: "business"}}
	if withHint.GoalsHash() == base {
		t.Error("hash should change when keyword hints change")
	}
}
