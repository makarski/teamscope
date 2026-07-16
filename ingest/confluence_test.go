package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPageTextFlattensTextAndEmoji(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","content":[
		{"type":"heading","content":[{"type":"text","text":"Feature Readiness"}]},
		{"type":"paragraph","content":[
			{"type":"emoji","attrs":{"text":"🟢"}},
			{"type":"text","text":"Core features complete"}
		]}
	]}`)

	text, err := pageText(body)
	if err != nil {
		t.Fatalf("pageText: %v", err)
	}
	if !strings.Contains(text, "Feature Readiness") {
		t.Errorf("heading text missing: %q", text)
	}
	if !strings.Contains(text, "🟢") || !strings.Contains(text, "Core features complete") {
		t.Errorf("emoji/body missing: %q", text)
	}
	// Heading and paragraph are separate blocks → separated by a newline.
	if !strings.Contains(text, "Feature Readiness\n") {
		t.Errorf("blocks not newline-separated: %q", text)
	}
}

func TestPageTextEmptyErrors(t *testing.T) {
	if _, err := pageText(json.RawMessage(`{"type":"doc","content":[]}`)); err == nil {
		t.Error("expected error for a page with no text")
	}
}

func TestDecodePillars(t *testing.T) {
	reply := `Here you go: {"pillars":[
		{"key":"feature-readiness","title":"Feature Readiness","done":true},
		{"key":"security","title":"Security & Compliance","done":false}
	]}`
	pillars, err := decodePillars(reply)
	if err != nil {
		t.Fatalf("decodePillars: %v", err)
	}
	if len(pillars) != 2 {
		t.Fatalf("want 2 pillars, got %+v", pillars)
	}
	if pillars[0].Key != "feature-readiness" || !pillars[0].Done {
		t.Errorf("pillar 0 = %+v", pillars[0])
	}
	if pillars[1].Done {
		t.Errorf("pillar 1 should be open: %+v", pillars[1])
	}
}

func TestDecodePillarsSkipsIncompleteEntries(t *testing.T) {
	reply := `{"pillars":[{"key":"","title":"No Key","done":true},{"key":"ok","title":"Fine","done":false}]}`
	pillars, err := decodePillars(reply)
	if err != nil {
		t.Fatalf("decodePillars: %v", err)
	}
	if len(pillars) != 1 || pillars[0].Key != "ok" {
		t.Errorf("want only the complete pillar, got %+v", pillars)
	}
}

func TestDecodePillarsErrors(t *testing.T) {
	cases := map[string]string{
		"not json":    "totally not json",
		"empty array": `{"pillars":[]}`,
		"all invalid": `{"pillars":[{"key":"","title":""}]}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodePillars(reply); err == nil {
				t.Errorf("expected error for %q", reply)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:               `{"a":1}`,
		`prefix {"a":1} suffix`: `{"a":1}`,
		`no braces`:             `no braces`,
	}
	for in, want := range cases {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

// stubCompleter returns a canned reply (or error) without any network call.
type stubCompleter struct {
	reply string
	err   error
}

func (s stubCompleter) Complete(context.Context, string, int) (string, error) {
	return s.reply, s.err
}

func TestFetchReadinessPillarsEndToEnd(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Feature Readiness: green"}]}]}`)
	client := &ConfluenceClient{
		fetch: func(string) (json.RawMessage, error) { return body, nil },
		ai:    stubCompleter{reply: `{"pillars":[{"key":"feature-readiness","title":"Feature Readiness","done":true}]}`},
	}

	pillars, err := client.FetchReadinessPillars("123")
	if err != nil {
		t.Fatalf("FetchReadinessPillars: %v", err)
	}
	if len(pillars) != 1 {
		t.Fatalf("got %d pillars, want 1: %+v", len(pillars), pillars)
	}
	if pillars[0].Key != "feature-readiness" {
		t.Errorf("key = %q, want feature-readiness", pillars[0].Key)
	}
	if !pillars[0].Done {
		t.Errorf("done = false, want true")
	}
}

func TestFetchReadinessPillarsPropagatesAIError(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]}`)
	client := &ConfluenceClient{
		fetch: func(string) (json.RawMessage, error) { return body, nil },
		ai:    stubCompleter{err: errors.New("ai down")},
	}
	if _, err := client.FetchReadinessPillars("123"); err == nil {
		t.Error("expected AI error to propagate")
	}
}

func TestFetchReadinessPillarsPropagatesFetchError(t *testing.T) {
	client := &ConfluenceClient{
		fetch: func(string) (json.RawMessage, error) { return nil, errors.New("404") },
		ai:    stubCompleter{reply: "{}"},
	}
	if _, err := client.FetchReadinessPillars("123"); err == nil {
		t.Error("expected fetch error to propagate")
	}
}

func TestExtractADFBody(t *testing.T) {
	payload := []byte(`{"body":{"atlas_doc_format":{"value":"{\"type\":\"doc\"}"}}}`)
	body, err := extractADFBody(payload)
	if err != nil {
		t.Fatalf("extractADFBody: %v", err)
	}
	if string(body) != `{"type":"doc"}` {
		t.Errorf("body = %s", body)
	}
}

func TestExtractADFBodyMissing(t *testing.T) {
	if _, err := extractADFBody([]byte(`{"body":{}}`)); err == nil {
		t.Error("expected error when atlas_doc_format body is absent")
	}
}
