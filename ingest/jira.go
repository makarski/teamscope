package ingest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"

	"github.com/makarski/teamscope/config"
	"github.com/makarski/teamscope/domain"
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
	startDateJQL   string
}

// defaultStartDateJQL is the standard JQL clause name for an epic start date.
const defaultStartDateJQL = `"Start date[Date]"`

// NewJiraClient builds a JIRA client from config.
func NewJiraClient(cfg *config.Jira) (*JiraClient, error) {
	tp := jira.BasicAuthTransport{Username: cfg.User, Password: cfg.Token}
	c, err := jira.NewClient(tp.Client(), cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("ingest: init jira client: %w", err)
	}
	startDateJQL := cfg.StartDateJQL
	if startDateJQL == "" {
		startDateJQL = defaultStartDateJQL
	}
	return &JiraClient{
		client:         c,
		startDateField: cfg.StartDateField,
		startDateJQL:   startDateJQL,
	}, nil
}

// FetchEpics returns all epics (with child issues) for a project that are in
// active scope this year: started this year, updated this year, or with no
// start date set. The permissive filter keeps roadsnap's "current work" intent
// while no longer silently dropping epics that simply lack a start date.
//
// Child issues are fetched in a single batched query (parent IN (...)) rather
// than one call per epic, to avoid N+1 HTTP round-trips on boards with many
// epics.
func (jc *JiraClient) FetchEpics(project string) ([]RawEpic, error) {
	raw, err := jc.searchActiveIssues(activeQuery{project: project, typeFilter: `issuetype = Epic`})
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch epics for %s: %w", project, err)
	}
	if len(raw) == 0 {
		return nil, nil
	}

	epicKeys := make([]string, 0, len(raw))
	epics := make([]RawEpic, 0, len(raw))
	for _, item := range raw {
		epicKeys = append(epicKeys, item.issue.Key)
		epics = append(epics, jc.rawEpicFromItem(item))
	}

	children, err := jc.fetchEpicChildren(epicKeys)
	if err != nil {
		return nil, err
	}
	for i := range epics {
		epics[i].Issues = children[epics[i].Epic.Key]
	}
	return epics, nil
}

// searchActiveIssues searches a project for issues matching typeFilter that are
// in active scope this year: started, updated, or undated. Shared by FetchEpics
// and FetchStandaloneIssues.
func (jc *JiraClient) searchActiveIssues(q activeQuery) ([]searchItem, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND %s AND (`+
			`%[3]s > startOfYear() OR `+
			`updated > startOfYear() OR `+
			`%[3]s IS EMPTY)`,
		q.project, q.typeFilter, jc.startDateJQL,
	)
	return jc.search(jql)
}

// activeQuery bundles the parameters for searchActiveIssues so the function
// signature stays free of string arguments.
type activeQuery struct {
	project    string
	typeFilter string
}

// FetchByKeys returns the live status of the given Jira issue keys, regardless
// of project. Used by the drift check to reconcile a readiness page's claims
// against actual ticket status. Keys without a match are silently omitted.
func (jc *JiraClient) FetchByKeys(keys []string) ([]domain.TicketLink, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	jql := "key IN (" + strings.Join(quoted, ", ") + ")"
	raw, err := jc.search(jql)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch by keys: %w", err)
	}

	out := make([]domain.TicketLink, 0, len(raw))
	for _, item := range raw {
		out = append(out, domain.TicketLink{
			Key:     item.issue.Key,
			Summary: item.issue.Fields.Summary,
			Status:  ticketStatus(item.issue),
		})
	}
	return out, nil
}

// ticketStatus derives a ProgressStatus from a jira.Issue's status name.
func ticketStatus(issue jira.Issue) domain.ProgressStatus {
	if issue.Fields.Status == nil {
		return domain.StatusToDo
	}
	name := issue.Fields.Status.Name
	switch {
	case strings.EqualFold(name, "Done"), strings.EqualFold(name, "Closed"), strings.EqualFold(name, "Resolved"):
		return domain.StatusDone
	case strings.EqualFold(name, "In Progress"), strings.EqualFold(name, "In Review"):
		return domain.StatusOngoing
	default:
		return domain.StatusToDo
	}
}

// FetchStandaloneIssues returns non-epic issues in a project that are in
// active scope this year and are not children of any epic. These are stories,
// tasks, bugs etc. that exist outside any epic — common on boards where not all
// work is organized under epics. excludeKeys is the set of issue keys already
// fetched as epic children, so they are not duplicated.
func (jc *JiraClient) FetchStandaloneIssues(project string, excludeKeys map[string]bool) ([]RawEpic, error) {
	raw, err := jc.searchActiveIssues(activeQuery{project: project, typeFilter: `issuetype != Epic`})
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch standalone issues for %s: %w", project, err)
	}

	issues := make([]RawEpic, 0, len(raw))
	for _, item := range raw {
		if excludeKeys[item.issue.Key] {
			continue
		}
		issues = append(issues, jc.rawEpicFromItem(item))
	}
	return issues, nil
}

// rawEpicFromItem builds a RawEpic (without child issues) from a search item.
func (jc *JiraClient) rawEpicFromItem(item searchItem) RawEpic {
	return RawEpic{
		Epic:        item.issue,
		DueDate:     time.Time(item.issue.Fields.Duedate),
		StartDate:   jc.parseStartDate(item.rawFields),
		description: item.description,
	}
}

// FetchByLabel returns the epics in a project carrying the given label,
// without child issues. Used to synthesize rubric criteria from Jira metadata.
func (jc *JiraClient) FetchByLabel(project, label string) ([]RawEpic, error) {
	jql := fmt.Sprintf(
		`project = "%s" AND issuetype = Epic AND labels = "%s"`,
		project, label,
	)
	raw, err := jc.search(jql)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch label %q in %s: %w", label, project, err)
	}

	epics := make([]RawEpic, 0, len(raw))
	for _, item := range raw {
		epics = append(epics, jc.rawEpicFromItem(item))
	}
	return epics, nil
}

// EpicStatus returns the epic's own Jira status name.
func (r *RawEpic) EpicStatus() string {
	if r.Epic.Fields.Status == nil {
		return ""
	}
	return r.Epic.Fields.Status.Name
}

// fetchEpicChildren fetches child issues for all epic keys in a single batched
// query (parent IN (...)) and groups them by parent key. Jira limits the IN
// clause length, so keys are chunked into batches of 100.
const epicChildBatchSize = 100

func (jc *JiraClient) fetchEpicChildren(epicKeys []string) (map[string][]jira.Issue, error) {
	children := make(map[string][]jira.Issue)
	for start := 0; start < len(epicKeys); start += epicChildBatchSize {
		end := start + epicChildBatchSize
		if end > len(epicKeys) {
			end = len(epicKeys)
		}
		batch := epicKeys[start:end]

		quoted := make([]string, len(batch))
		for i, k := range batch {
			quoted[i] = fmt.Sprintf("%q", k)
		}
		jql := "parent IN (" + strings.Join(quoted, ", ") + ")"
		raw, err := jc.search(jql)
		if err != nil {
			return nil, fmt.Errorf("ingest: fetch epic children: %w", err)
		}
		for _, item := range raw {
			parent := item.issue.Fields.Parent.Key
			children[parent] = append(children[parent], item.issue)
		}
	}
	return children, nil
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

	var allIssues []json.RawMessage
	nextPageToken := ""

	for {
		params := url.Values{}
		params.Add("jql", jql)
		params.Add("fields", "*all,-comment")
		params.Add("expand", "changelog,names")
		if nextPageToken != "" {
			params.Add("nextPageToken", nextPageToken)
		}

		req, err := jc.client.NewRequest("GET", endpoint+"?"+params.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("ingest: build request: %w", err)
		}

		var response struct {
			Issues        []json.RawMessage `json:"issues"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if _, err := jc.client.Do(req, &response); err != nil {
			return nil, fmt.Errorf("ingest: execute jql %q: %w", jql, err)
		}

		allIssues = append(allIssues, response.Issues...)
		if response.NextPageToken == "" {
			break
		}
		nextPageToken = response.NextPageToken
	}

	return decodeSearchItems(allIssues)
}

func decodeSearchItems(rawIssues []json.RawMessage) ([]searchItem, error) {
	items := make([]searchItem, 0, len(rawIssues))
	for _, raw := range rawIssues {
		rawFields, err := decodeRawFields(raw)
		if err != nil {
			return nil, err
		}

		description := adfPlainText(rawFields["description"])

		// Strip ADF-typed fields before decoding into jira.Issue: Jira API v3
		// returns them as ADF objects, but go-jira expects plain strings.
		issueJSON, err := stripADFFields(raw)
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

// adfFields are Jira API v3 fields returned as ADF objects that go-jira types
// as strings. They must be stripped from the raw JSON before decoding into
// jira.Issue, otherwise unmarshalling fails.
var adfFields = []string{"description", "environment"}

// stripADFFields removes ADF-typed fields from the raw issue JSON so the
// go-jira Issue struct (which types them as strings) can decode without error.
func stripADFFields(raw json.RawMessage) (json.RawMessage, error) {
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
	for _, f := range adfFields {
		delete(fieldMap, f)
	}

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
