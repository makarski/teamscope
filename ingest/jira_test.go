package ingest

import (
	"encoding/json"
	"testing"

	"github.com/andygrunwald/go-jira"
)

func TestAdfPlainText(t *testing.T) {
	adf := `{
		"type": "doc",
		"content": [
			{"type": "paragraph", "content": [
				{"type": "text", "text": "Refactor the"},
				{"type": "text", "text": " billing engine"}
			]},
			{"type": "paragraph", "content": [
				{"type": "text", "text": "for revenue"}
			]}
		]
	}`
	got := adfPlainText(json.RawMessage(adf))
	want := "Refactor the  billing engine for revenue"
	if got != want {
		t.Errorf("adfPlainText = %q, want %q", got, want)
	}
}

func TestAdfPlainTextEmpty(t *testing.T) {
	if got := adfPlainText(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := adfPlainText(json.RawMessage(`"plain string"`)); got != "" {
		t.Errorf("non-object = %q, want empty", got)
	}
}

func TestStripDescriptionAllowsADFDecode(t *testing.T) {
	// description as an ADF object would break a plain-string decode.
	raw := json.RawMessage(`{
		"key": "AP-1",
		"fields": {
			"summary": "Access work",
			"description": {"type": "doc", "content": []}
		}
	}`)

	stripped, err := stripADFFields(raw)
	if err != nil {
		t.Fatalf("stripADFFields: %v", err)
	}

	var issue jira.Issue
	if err := json.Unmarshal(stripped, &issue); err != nil {
		t.Fatalf("decode after strip: %v", err)
	}
	if issue.Key != "AP-1" || issue.Fields.Summary != "Access work" {
		t.Errorf("issue decoded wrong: %+v", issue)
	}
}

func TestDecodeSearchItemsWithADFDescription(t *testing.T) {
	rawIssues := []json.RawMessage{json.RawMessage(`{
		"key": "AP-1",
		"fields": {
			"summary": "Access work",
			"description": {"type": "doc", "content": [
				{"type": "paragraph", "content": [{"type": "text", "text": "onboarding flow"}]}
			]}
		}
	}`)}

	items, err := decodeSearchItems(rawIssues)
	if err != nil {
		t.Fatalf("decodeSearchItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].description != "onboarding flow" {
		t.Errorf("description = %q, want %q", items[0].description, "onboarding flow")
	}
	if items[0].issue.Key != "AP-1" {
		t.Errorf("key = %q, want AP-1", items[0].issue.Key)
	}
}
