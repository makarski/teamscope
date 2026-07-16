package align

import "testing"

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean", `{"advances":true,"note":"x"}`, `{"advances":true,"note":"x"}`},
		{"wrapped in prose", `Sure: {"advances":false,"note":"y"} done`, `{"advances":false,"note":"y"}`},
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
	reply, err := parseReply(`prefix {"advances":true,"note":"delivers the pillar"} suffix`)
	if err != nil {
		t.Fatalf("parseReply: %v", err)
	}
	if reply.Advances == nil {
		t.Fatal("advances should be present")
	}
	if !*reply.Advances || reply.Note != "delivers the pillar" {
		t.Errorf("unexpected reply: %+v", reply)
	}
}

func TestParseReplyMissingAdvances(t *testing.T) {
	if _, err := parseReply(`{"note":"no verdict"}`); err == nil {
		t.Error("expected error when advances field is absent")
	}
}

func TestParseReplyInvalid(t *testing.T) {
	if _, err := parseReply("not json at all"); err == nil {
		t.Error("expected error for non-json reply")
	}
}
