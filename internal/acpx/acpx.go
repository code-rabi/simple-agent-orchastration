package acpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

type Runner struct {
	Command []string
}

type Result struct {
	AssistantText string
	StopReason    string
	RawLines      []string
}

type claudeResult struct {
	Result     string `json:"result"`
	StopReason string `json:"stop_reason"`
}

func ResolveAgentName(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude", "claudecode", "claude-code":
		return "claude", true
	case "codex":
		return "codex", true
	default:
		return "", false
	}
}

func NewRunner(command []string) Runner {
	return Runner{Command: command}
}

func (r Runner) Exec(ctx context.Context, cwd string, agent string, prompt string) (Result, error) {
	if len(r.Command) == 0 {
		return Result{}, errors.New("agent command is not configured")
	}

	switch agent {
	case "codex":
		return r.execCodex(ctx, cwd, prompt)
	case "claude":
		return r.execClaude(ctx, cwd, prompt)
	default:
		return Result{}, fmt.Errorf("unsupported agent runtime %q", agent)
	}
}

func (r Runner) execCodex(ctx context.Context, cwd string, prompt string) (Result, error) {
	outputFile, err := os.CreateTemp("", "sao-codex-last-message-*.txt")
	if err != nil {
		return Result{}, fmt.Errorf("create codex output file: %w", err)
	}
	outputPath := outputFile.Name()
	if closeErr := outputFile.Close(); closeErr != nil {
		_ = os.Remove(outputPath)
		return Result{}, fmt.Errorf("close codex output file: %w", closeErr)
	}
	defer os.Remove(outputPath)

	args := append([]string{}, r.Command[1:]...)
	args = append(args,
		"exec",
		"--cd", cwd,
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"--json",
		"--output-last-message", outputPath,
		prompt,
	)

	cmd := exec.CommandContext(ctx, r.Command[0], args...)
	cmd.Dir = cwd
	cmd.Env = buildEnv("codex")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return Result{}, errors.New(msg)
	}

	result := Result{
		AssistantText: strings.TrimSpace(readFileIfPresent(outputPath)),
		RawLines:      collectLines(stdout.String()),
	}
	parseCodexStopReason(stdout.Bytes(), &result)
	return result, nil
}

func (r Runner) execClaude(ctx context.Context, cwd string, prompt string) (Result, error) {
	args := append([]string{}, r.Command[1:]...)
	args = append(args,
		"--print",
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
		"--dangerously-skip-permissions",
		prompt,
	)

	cmd := exec.CommandContext(ctx, r.Command[0], args...)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return Result{}, errors.New(msg)
	}

	result, err := parseClaudeResult(stdout.Bytes())
	if err != nil {
		return Result{}, err
	}
	result.RawLines = collectLines(stdout.String())
	return result, nil
}

func buildEnv(agent string) []string {
	env := os.Environ()
	switch strings.ToLower(agent) {
	case "codex":
		if hasEnv(env, "CODEX_HOME") {
			return env
		}
		if currentUser, err := user.Current(); err == nil && currentUser.HomeDir != "" {
			env = append(env, "CODEX_HOME="+filepath.Join(currentUser.HomeDir, ".codex"))
		}
	}
	return env
}

func hasEnv(env []string, key string) bool {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func readFileIfPresent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func collectLines(output string) []string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	lines := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseCodexStopReason(output []byte, result *Result) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if asString(payload["type"]) != "turn.completed" {
			continue
		}
		result.StopReason = "end_turn"
	}
}

func parseClaudeResult(output []byte) (Result, error) {
	var payload claudeResult
	if err := json.Unmarshal(bytes.TrimSpace(output), &payload); err != nil {
		return Result{}, fmt.Errorf("parse claude output: %w", err)
	}
	return Result{
		AssistantText: strings.TrimSpace(payload.Result),
		StopReason:    strings.TrimSpace(payload.StopReason),
	}, nil
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}
