package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/config"
)

const (
	DefaultBaseURL = "http://localhost:3284"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Session struct {
	cmd    *exec.Cmd
	client Client
}

type Message struct {
	ID      string `json:"id,omitempty"`
	Role    string `json:"role,omitempty"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content"`
	Time    string `json:"time,omitempty"`
}

type Status struct {
	AgentType string `json:"agent_type,omitempty"`
	Status    string `json:"status"`
	Transport string `json:"transport,omitempty"`
}

type messageEnvelope struct {
	Messages []Message `json:"messages"`
}

func NewClient(baseURL string) Client {
	return Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{},
	}
}

func StartSession(ctx context.Context, agentapiPath string, workDir string, agent config.InstalledAgent) (*Session, error) {
	args := []string{"server"}
	if agent.Type != "" {
		args = append(args, "--type="+agent.Type)
	}
	args = append(args, "--")
	args = append(args, agent.Command...)

	cmd := exec.CommandContext(ctx, agentapiPath, args...)
	cmd.Dir = workDir
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agentapi: %w", err)
	}

	session := &Session{
		cmd:    cmd,
		client: NewClient(DefaultBaseURL),
	}
	if err := session.waitUntilReady(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

func (s *Session) Close() error {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	_, _ = s.cmd.Process.Wait()
	return nil
}

func (s *Session) SendAndWait(ctx context.Context, prompt string, maxWait time.Duration) (string, error) {
	if err := s.sendMessageWhenReady(ctx, Message{
		Type:    "user",
		Content: prompt,
	}); err != nil {
		return "", err
	}

	deadline := time.Now().Add(maxWait)
	seenRunning := false
	for {
		if time.Now().After(deadline) {
			return "", errors.New("timed out waiting for agentapi session to settle")
		}
		status, err := s.client.Status(ctx)
		if err != nil {
			return "", err
		}
		if status.Status == "running" {
			seenRunning = true
		}
		if seenRunning && status.Status == "stable" {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	messages, err := s.client.Messages(ctx)
	if err != nil {
		return "", err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" || messages[i].Role == "agent" {
			return strings.TrimSpace(messages[i].Content), nil
		}
	}
	if len(messages) > 0 {
		return strings.TrimSpace(messages[len(messages)-1].Content), nil
	}
	return "", nil
}

func (s *Session) waitUntilReady(ctx context.Context) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for agentapi to become ready")
		}
		_, err := s.client.Status(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Session) sendMessageWhenReady(ctx context.Context, message Message) error {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for {
		err := s.client.SendMessage(ctx, message)
		if err == nil {
			return nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "waiting for user input") {
			return err
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c Client) SendMessage(ctx context.Context, message Message) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/message", message, nil)
	return err
}

func (c Client) Messages(ctx context.Context) ([]Message, error) {
	var envelope messageEnvelope
	_, err := c.doJSON(ctx, http.MethodGet, "/messages", nil, &envelope)
	return envelope.Messages, err
}

func (c Client) Status(ctx context.Context) (Status, error) {
	var status Status
	_, err := c.doJSON(ctx, http.MethodGet, "/status", nil, &status)
	return status, err
}

func (c Client) doJSON(ctx context.Context, method, path string, requestBody any, responseBody any) (*http.Response, error) {
	var body io.Reader
	if requestBody != nil {
		data, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agentapi %s %s failed: %s", method, path, strings.TrimSpace(string(data)))
	}
	if responseBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
			return nil, fmt.Errorf("decode response %s %s: %w", method, path, err)
		}
	}
	return resp, nil
}
