package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml"
)

const DefaultFileName = "teamscope-config.toml"

type (
	Config struct {
		Jira      *Jira      `toml:"jira"`
		GitHub    *GitHub    `toml:"github"`
		Anthropic *Anthropic `toml:"anthropic"`
		Bedrock   *Bedrock   `toml:"bedrock"`
		Slack     *Slack     `toml:"slack"`
		Rubrics   []Rubric   `toml:"rubrics"`
		Teams     []Team     `toml:"teams"`
		Store     *Store     `toml:"store"`
	}

	Jira struct {
		User    string `toml:"user"`
		BaseURL string `toml:"base_url"`
		Token   string `toml:"token"`
		// StartDateField is the custom field id holding an epic start date.
		StartDateField string `toml:"start_date_field"`
		// StatusNames maps raw Jira statuses onto done/progress/todo buckets.
		StatusNames StatusNames `toml:"status_names"`
	}

	StatusNames struct {
		Done       []string `toml:"done"`
		InProgress []string `toml:"progress"`
		ToDo       []string `toml:"todo"`
	}

	GitHub struct {
		Token string `toml:"token"`
	}

	Anthropic struct {
		BaseURL string `toml:"base_url"`
		Token   string `toml:"token"`
		Model   string `toml:"model"`
	}

	// Bedrock runs the AI stage through Amazon Bedrock instead of the
	// Anthropic API directly. Auth uses the standard AWS credential chain
	// (env vars, shared config/profile, or an IAM role) — no token here.
	// When both [anthropic] and [bedrock] are configured, Bedrock wins.
	Bedrock struct {
		Region  string `toml:"region"`
		Model   string `toml:"model"`
		Profile string `toml:"profile"`
	}

	Slack struct {
		Token   string `toml:"token"`
		Channel string `toml:"channel"`
	}

	// Rubric defines a set of criteria a team is measured against, resolved
	// from a pluggable source.
	//
	//   source = "static"      criteria are listed inline below
	//   source = "jira_label"  criteria are the epics carrying Label in
	//                          LabelProject (one criterion per epic)
	Rubric struct {
		Name         string        `toml:"name"`
		Source       string        `toml:"source"`
		Lens         string        `toml:"lens"`
		Label        string        `toml:"label"`
		LabelProject string        `toml:"label_project"`
		Criteria     []Criterion   `toml:"criteria"`
		KeywordHints []KeywordHint `toml:"keyword_hints"`
	}

	Criterion struct {
		Key    string  `toml:"key"`
		Title  string  `toml:"title"`
		Status string  `toml:"status"`
		Weight float64 `toml:"weight"`
		Lens   string  `toml:"lens"`
	}

	KeywordHint struct {
		Keyword   string `toml:"keyword"`
		Criterion string `toml:"criterion"`
	}

	Team struct {
		Name         string   `toml:"name"`
		JiraProjects []string `toml:"jira_projects"`
		GitHubRepos  []string `toml:"github_repos"`
		Rubric       string   `toml:"rubric"`
	}

	Store struct {
		Path string `toml:"path"`
	}
)

func LoadConfig(filepath string) (*Config, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %s. %s", filepath, err)
	}
	defer f.Close()

	var cfg Config
	if err := toml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %s", err)
	}

	cfg.clearPlaceholders()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// isPlaceholder reports whether a value is an unedited template placeholder,
// i.e. wrapped in angle brackets like "<jira-api-token>".
func isPlaceholder(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">")
}

func blankIfPlaceholder(v string) string {
	if isPlaceholder(v) {
		return ""
	}
	return v
}

// clearPlaceholders blanks out unedited template placeholders in optional
// sections so a half-filled template never triggers a real API call.
func (c *Config) clearPlaceholders() {
	if c.Slack != nil {
		c.Slack.Token = blankIfPlaceholder(c.Slack.Token)
		c.Slack.Channel = blankIfPlaceholder(c.Slack.Channel)
	}
	if c.Anthropic != nil {
		c.Anthropic.Token = blankIfPlaceholder(c.Anthropic.Token)
	}
	if c.Bedrock != nil {
		c.Bedrock.Model = blankIfPlaceholder(c.Bedrock.Model)
	}
	if c.GitHub != nil {
		c.GitHub.Token = blankIfPlaceholder(c.GitHub.Token)
	}
}

func (c *Config) validate() error {
	if c.Jira == nil || c.Jira.BaseURL == "" {
		return fmt.Errorf("config: [jira] base_url is required")
	}
	if len(c.Teams) == 0 {
		return fmt.Errorf("config: at least one [[teams]] entry is required")
	}
	if err := c.validateTeams(); err != nil {
		return err
	}
	if c.Store == nil || c.Store.Path == "" {
		return fmt.Errorf("config: [store] path is required")
	}
	return nil
}

func (c *Config) validateTeams() error {
	for _, t := range c.Teams {
		if t.Name == "" {
			return fmt.Errorf("config: every team requires a name")
		}
		if len(t.JiraProjects) == 0 {
			return fmt.Errorf("config: team %q requires at least one jira_project", t.Name)
		}
		if t.Rubric == "" {
			return fmt.Errorf("config: team %q requires a rubric", t.Name)
		}
		if _, ok := c.RubricByName(t.Rubric); !ok {
			return fmt.Errorf("config: team %q references unknown rubric %q", t.Name, t.Rubric)
		}
	}
	return nil
}

// RubricByName looks up a configured rubric by name.
func (c *Config) RubricByName(name string) (Rubric, bool) {
	for _, r := range c.Rubrics {
		if r.Name == name {
			return r, true
		}
	}
	return Rubric{}, false
}

// GoalsHash returns a stable fingerprint of the rubric definitions so
// snapshots can be tied to the goals configuration that produced them.
func (c *Config) GoalsHash() string {
	var b strings.Builder
	for _, r := range c.Rubrics {
		fmt.Fprintf(&b, "%s|%s|%s|%s;", r.Name, r.Source, r.Label, r.LabelProject)
		for _, cr := range r.Criteria {
			fmt.Fprintf(&b, "%s=%s,", cr.Key, cr.Title)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}
