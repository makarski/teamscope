package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
)

// pageText flattens an ADF document into plain text for the AI to read. It
// concatenates all text nodes and emoji glyphs; block boundaries (paragraphs,
// list items, table cells, headings) become newlines so structure survives.
func pageText(body json.RawMessage) (string, error) {
	var doc adfNode
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("ingest: decode confluence body: %w", err)
	}

	var b strings.Builder
	writeText(doc, &b)

	text := strings.TrimSpace(collapseBlankLines(b.String()))
	if text == "" {
		return "", fmt.Errorf("ingest: confluence page has no text")
	}
	return text, nil
}

// adfNode is the minimal shape needed to flatten a document: node type, text,
// emoji glyph (in attrs), and child content.
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Attrs   adfAttrs  `json:"attrs"`
	Content []adfNode `json:"content"`
}

type adfAttrs struct {
	Text string `json:"text"`
}

// blockTypes end with a newline so the flattened text keeps its structure.
var blockTypes = map[string]bool{
	"paragraph":   true,
	"heading":     true,
	"listItem":    true,
	"tableRow":    true,
	"tableCell":   true,
	"tableHeader": true,
}

func writeText(n adfNode, b *strings.Builder) {
	if glyph := glyphOf(n); glyph != "" {
		b.WriteString(glyph)
		b.WriteString(" ")
	}
	for _, child := range n.Content {
		writeText(child, b)
	}
	if blockTypes[n.Type] {
		b.WriteString("\n")
	}
}

// glyphOf returns a leaf node's renderable text: a text node's text, or an
// emoji node's glyph (which lives in its attributes).
func glyphOf(n adfNode) string {
	if n.Text != "" {
		return n.Text
	}
	if n.Type == "emoji" {
		return n.Attrs.Text
	}
	return ""
}

// collapseBlankLines squeezes runs of blank lines into a single newline so the
// prompt stays compact.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, trimmed)
	}
	return strings.Join(out, "\n")
}
