package align

import (
	"testing"

	"github.com/makarski/teamscope/domain"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean", `{"alignment":"aligned","note":"x"}`, `{"alignment":"aligned","note":"x"}`},
		{"wrapped in prose", `Sure: {"alignment":"partial","note":"y"} done`, `{"alignment":"partial","note":"y"}`},
		{"no braces returns raw", `nonsense`, `nonsense`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractJSON(tt.in); got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseReply(t *testing.T) {
	reply, err := parseReply(`prefix {"alignment":"off_track","note":"unrelated to goals"} suffix`)
	if err != nil {
		t.Fatalf("parseReply: %v", err)
	}
	if reply.Alignment != "off_track" || reply.Note != "unrelated to goals" {
		t.Errorf("unexpected reply: %+v", reply)
	}
}

func TestParseReplyInvalid(t *testing.T) {
	if _, err := parseReply("not json at all"); err == nil {
		t.Error("expected error for non-json reply")
	}
}

func TestNormalizeAlignment(t *testing.T) {
	tests := []struct {
		in      string
		want    domain.Alignment
		wantErr bool
	}{
		{"aligned", domain.AlignAligned, false},
		{"  Partial ", domain.AlignPartial, false},
		{"OFF_TRACK", domain.AlignOffTrack, false},
		{"maybe", "", true},
	}
	for _, tt := range tests {
		got, err := normalizeAlignment(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("normalizeAlignment(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("normalizeAlignment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
