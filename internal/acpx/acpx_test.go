package acpx

import "testing"

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
		{name: "trim and lowercase", input: " Claude ", want: "claude", ok: true},
		{name: "unsupported", input: "gemini", want: "", ok: false},
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

func TestParseClaudeResult(t *testing.T) {
	t.Parallel()

	got, err := parseClaudeResult([]byte(`{"result":"hi","stop_reason":"end_turn"}`))
	if err != nil {
		t.Fatalf("parseClaudeResult() error = %v", err)
	}
	if got.AssistantText != "hi" {
		t.Fatalf("AssistantText = %q, want %q", got.AssistantText, "hi")
	}
	if got.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", got.StopReason, "end_turn")
	}
}

func TestCollectLines(t *testing.T) {
	t.Parallel()

	got := collectLines("\nalpha\n\n beta \n")
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("collectLines() = %#v, want []string{\"alpha\", \"beta\"}", got)
	}
}
