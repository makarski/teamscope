package ingest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
)

// RawEpic is an epic together with its child issues and parsed planning dates,
// prior to classification and alignment scoring.
type RawEpic struct {
	Epic        jira.Issue
	Issues      []jira.Issue
	StartDate   time.Time
	DueDate     time.Time
	description string
}

// Labels returns the epic's Jira labels.
func (r *RawEpic) Labels() []string { return r.Epic.Fields.Labels }

// Components returns the epic's Jira component names.
func (r *RawEpic) Components() []string {
	names := make([]string, 0, len(r.Epic.Fields.Components))
	for _, c := range r.Epic.Fields.Components {
		names = append(names, c.Name)
	}
	return names
}

// Text returns the concatenated summary and description for keyword/AI matching.
func (r *RawEpic) Text() string {
	return r.Epic.Fields.Summary + "\n" + r.description
}

// NewRawEpic builds a RawEpic with an explicit plain-text description.
// Primarily intended for tests and callers outside this package.
func NewRawEpic(epic jira.Issue, issues []jira.Issue, description string) RawEpic {
	return RawEpic{Epic: epic, Issues: issues, description: description}
}

// JiraClient fetches epics and their issues for a project.
type JiraClient struct {
	client         *jira.Client
	startDateField string
}

// NewJiraClient builds a JIRA client from config.
func NewJiraClient(cfg *config.Jira) (*JiraClient, error) {
	tp := jira.BasicAuthTransport{Username: cfg.User, Password: cfg.Token}
	c, err := jira.NewClient(tp.Client(), cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("ingest: init jira client: %w", err)
	}
	return &JiraClient{client: c, startDateField: cfg.StartDateField}, nil
}

// FetchEpics returns all epics (with child issues) for a project that started
// this year, mirroring roadsnap's roadmap query.
func (jc *JiraClient) FetchEpics(project string) ([]RawEpic, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND issuetype = Epic AND "Start date[Date]" > startOfYear()`,
		project,
	)
	raw, err := jc.search(jql)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch epics for %s: %w", project, err)
	}

	epics := make([]RawEpic, 0, len(raw))
	for _, item := range raw {
		re := RawEpic{
			Epic:        item.issue,
			DueDate:     time.Time(item.issue.Fields.Duedate),
			StartDate:   jc.parseStartDate(item.rawFields),
			description: item.description,
		}

		issues, err := jc.fetchEpicIssues(item.issue.Key)
		if err != nil {
			return nil, err
		}
		re.Issues = issues
		epics = append(epics, re)
	}
	return epics, nil
}

func (jc *JiraClient) fetchEpicIssues(epicKey string) ([]jira.Issue, error) {
	raw, err := jc.search(fmt.Sprintf("parent = %s", epicKey))
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch issues for epic %s: %w", epicKey, err)
	}
	issues := make([]jira.Issue, 0, len(raw))
	for _, item := range raw {
		issues = append(issues, item.issue)
	}
	return issues, nil
}

func (jc *JiraClient) parseStartDate(rawFields map[string]json.RawMessage) time.Time {
	if jc.startDateField == "" {
		return time.Time{}
	}
	rawVal, ok := rawFields[jc.startDateField]
	if !ok {
		return time.Time{}
	}
	var s string
	if err := json.Unmarshal(rawVal, &s); err != nil {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// searchItem pairs a decoded issue with its raw field map so custom fields
// (e.g. start date) remain accessible, plus the plain-text description
// extracted from Jira's ADF representation.
type searchItem struct {
	issue       jira.Issue
	rawFields   map[string]json.RawMessage
	description string
}

func (jc *JiraClient) search(jql string) ([]searchItem, error) {
	endpoint := "/rest/api/3/search/jql"

	params := url.Values{}
	params.Add("jql", jql)
	params.Add("fields", "*all,-comment")
	params.Add("expand", "changelog,names")

	req, err := jc.client.NewRequest("GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("ingest: build request: %w", err)
	}

	var response struct {
		Issues []json.RawMessage `json:"issues"`
	}
	if _, err := jc.client.Do(req, &response); err != nil {
		return nil, fmt.Errorf("ingest: execute jql %q: %w", jql, err)
	}

	return decodeSearchItems(response.Issues)
}

func decodeSearchItems(rawIssues []json.RawMessage) ([]searchItem, error) {
	items := make([]searchItem, 0, len(rawIssues))
	for _, raw := range rawIssues {
		rawFields, err := decodeRawFields(raw)
		if err != nil {
			return nil, err
		}

		description := adfPlainText(rawFields["description"])

		// Strip description before decoding into jira.Issue: Jira API v3
		// returns it as an ADF object, but go-jira expects a plain string.
		issueJSON, err := stripDescription(raw)
		if err != nil {
			return nil, err
		}

		var issue jira.Issue
		if err := json.Unmarshal(issueJSON, &issue); err != nil {
			return nil, fmt.Errorf("ingest: decode issue: %w", err)
		}

		items = append(items, searchItem{
			issue:       issue,
			rawFields:   rawFields,
			description: description,
		})
	}
	return items, nil
}

func decodeRawFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var envelope struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("ingest: decode issue fields: %w", err)
	}
	return envelope.Fields, nil
}

// stripDescription removes fields.description from the raw issue JSON so the
// go-jira Issue struct (which types description as a string) can decode ADF
// projects without error.
func stripDescription(raw json.RawMessage) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("ingest: decode issue envelope: %w", err)
	}

	fields, ok := envelope["fields"]
	if !ok {
		return raw, nil
	}

	var fieldMap map[string]json.RawMessage
	if err := json.Unmarshal(fields, &fieldMap); err != nil {
		return nil, fmt.Errorf("ingest: decode fields map: %w", err)
	}
	delete(fieldMap, "description")

	patchedFields, err := json.Marshal(fieldMap)
	if err != nil {
		return nil, fmt.Errorf("ingest: re-marshal fields: %w", err)
	}
	envelope["fields"] = patchedFields

	patched, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("ingest: re-marshal issue: %w", err)
	}
	return patched, nil
}

// adfPlainText walks a Jira ADF document and concatenates all text nodes.
// Non-object or empty input yields an empty string.
func adfPlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	var b strings.Builder
	collectADFText(node, &b)
	return strings.TrimSpace(b.String())
}

func collectADFText(node any, b *strings.Builder) {
	switch v := node.(type) {
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			b.WriteString(text)
			b.WriteString(" ")
		}
		if content, ok := v["content"].([]any); ok {
			collectADFText(content, b)
		}
	case []any:
		for _, child := range v {
			collectADFText(child, b)
		}
	}
}
