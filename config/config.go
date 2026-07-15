package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/pelletier/go-toml"
)

const DefaultFileName = "teamscope-config.toml"

type (
	Config struct {
		Jira      *Jira      `toml:"jira"`
		GitHub    *GitHub    `toml:"github"`
		Anthropic *Anthropic `toml:"anthropic"`
		Slack     *Slack     `toml:"slack"`
		Goals     *Goals     `toml:"goals"`
		Classify  *Classify  `toml:"classify"`
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

	Slack struct {
		Token   string `toml:"token"`
		Channel string `toml:"channel"`
	}

	Goals struct {
		Prompt string `toml:"prompt"`
	}

	// Classify holds keyword hints per work type used by the rule engine.
	Classify struct {
		Business []string `toml:"business"`
		Chore    []string `toml:"chore"`
		RnD      []string `toml:"rnd"`
	}

	Team struct {
		Name         string   `toml:"name"`
		JiraProjects []string `toml:"jira_projects"`
		GitHubRepos  []string `toml:"github_repos"`
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

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Jira == nil || c.Jira.BaseURL == "" {
		return fmt.Errorf("config: [jira] base_url is required")
	}
	if len(c.Teams) == 0 {
		return fmt.Errorf("config: at least one [[teams]] entry is required")
	}
	for _, t := range c.Teams {
		if t.Name == "" {
			return fmt.Errorf("config: every team requires a name")
		}
		if len(t.JiraProjects) == 0 {
			return fmt.Errorf("config: team %q requires at least one jira_project", t.Name)
		}
	}
	if c.Store == nil || c.Store.Path == "" {
		return fmt.Errorf("config: [store] path is required")
	}
	return nil
}

// GoalsHash returns a stable fingerprint of the current goals prompt so
// snapshots can be tied to the goals definition that produced them.
func (c *Config) GoalsHash() string {
	prompt := ""
	if c.Goals != nil {
		prompt = c.Goals.Prompt
	}
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:8])
}
