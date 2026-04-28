package acpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type rpcMessage struct {
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Params json.RawMessage `json:"params"`
	Error  json.RawMessage `json:"error"`
}

type sessionUpdateEnvelope struct {
	Update sessionUpdate `json:"update"`
}

type sessionUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       contentBlock    `json:"content"`
	ToolCall      json.RawMessage `json:"toolCall"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

func ResolveCommand(ctx context.Context) ([]string, error) {
	if path, err := exec.LookPath("acpx"); err == nil {
		return []string{path}, nil
	}
	if path, err := exec.LookPath("npx"); err == nil {
		return []string{path, "-y", "acpx@latest"}, nil
	}
	return nil, errors.New("neither acpx nor npx is available on PATH")
}

func NewRunner(command []string) Runner {
	return Runner{Command: command}
}

func (r Runner) Exec(ctx context.Context, cwd string, agent string, prompt string) (Result, error) {
	if len(r.Command) == 0 {
		return Result{}, errors.New("acpx command is not configured")
	}

	args := append([]string{}, r.Command[1:]...)
	args = append(args,
		"--cwd", cwd,
		"--approve-all",
		"--format", "json",
		"--json-strict",
		agent,
		"exec",
		prompt,
	)

	cmd := exec.CommandContext(ctx, r.Command[0], args...)
	cmd.Dir = cwd
	cmd.Env = buildEnv(agent)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open acpx stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start acpx: %w", err)
	}

	result, parseErr := parseNDJSON(stdout)
	waitErr := cmd.Wait()

	if parseErr != nil {
		return Result{}, parseErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return Result{}, errors.New(msg)
	}
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

func parseNDJSON(r io.Reader) (Result, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var result Result
	var assistant strings.Builder

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		result.RawLines = append(result.RawLines, line)

		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return Result{}, fmt.Errorf("parse acpx line: %w", err)
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return Result{}, fmt.Errorf("acpx error response: %s", string(msg.Error))
		}

		if msg.Method == "session/update" {
			var env sessionUpdateEnvelope
			if err := json.Unmarshal(msg.Params, &env); err != nil {
				return Result{}, fmt.Errorf("parse acpx session update: %w", err)
			}
			if env.Update.SessionUpdate == "agent_message_chunk" && env.Update.Content.Type == "text" {
				assistant.WriteString(env.Update.Content.Text)
			}
			continue
		}

		if len(msg.Result) > 0 && string(msg.Result) != "null" {
			var pr promptResult
			if err := json.Unmarshal(msg.Result, &pr); err == nil && pr.StopReason != "" {
				result.StopReason = pr.StopReason
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("read acpx output: %w", err)
	}

	result.AssistantText = strings.TrimSpace(assistant.String())
	return result, nil
}
