package acpx

import (
	"encoding/json"
	"testing"
)

func TestResolveAgentName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "codex", input: "codex", want: "codex", ok: true},
		{name: "claude type alias", input: "claude-code", want: "claude", ok: true},
		{name: "trim and lowercase", input: " Gemini ", want: "gemini", ok: true},
		{name: "unsupported", input: "cursor", want: "", ok: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ResolveAgentName(tc.input)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("ResolveAgentName(%q) = (%q, %t), want (%q, %t)", tc.input, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestExtractText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "single text block",
			raw:  `{"type":"text","text":"hello"}`,
			want: "hello",
		},
		{
			name: "array of text blocks",
			raw:  `[{"type":"text","text":"hello"},{"type":"text","text":" world"}]`,
			want: "hello world",
		},
		{
			name: "resource link falls back to title",
			raw:  `{"type":"resource_link","title":"README.md","uri":"file:///README.md"}`,
			want: "README.md",
		},
		{
			name: "resource falls back to embedded text",
			raw:  `{"type":"resource","resource":{"text":"embedded"}}`,
			want: "embedded",
		},
		{
			name: "nested content array",
			raw:  `{"content":[{"type":"text","text":"hello"},{"type":"text","text":" again"}]}`,
			want: "hello again",
		},
		{
			name: "unknown shape is ignored",
			raw:  `{"type":"image","data":"abc123"}`,
			want: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractText(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("extractText() = %q, want %q", got, tc.want)
			}
		})
	}
}
