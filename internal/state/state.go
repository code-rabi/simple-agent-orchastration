package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/config"
)

const stateFileName = "state.json"

type Store struct {
	Tasks map[string]TaskRecord `json:"tasks"`
}

type TaskRecord struct {
	IssueURL       string    `json:"issue_url"`
	RepoPath       string    `json:"repo_path"`
	AgentName      string    `json:"agent_name"`
	Status         string    `json:"status"`
	IssueUpdatedAt time.Time `json:"issue_updated_at,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	LastResponse   string    `json:"last_response,omitempty"`
	Branch         string    `json:"branch,omitempty"`
	WorktreePath   string    `json:"worktree_path,omitempty"`
	CommitSHA      string    `json:"commit_sha,omitempty"`
	PullRequestURL string    `json:"pull_request_url,omitempty"`
}

func Load() (Store, error) {
	path, err := Path()
	if err != nil {
		return Store{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Store{Tasks: map[string]TaskRecord{}}, nil
	}
	if err != nil {
		return Store{}, fmt.Errorf("read state file: %w", err)
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("parse state file: %w", err)
	}
	if store.Tasks == nil {
		store.Tasks = map[string]TaskRecord{}
	}
	return store, nil
}

func Save(store Store) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if store.Tasks == nil {
		store.Tasks = map[string]TaskRecord{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

func Path() (string, error) {
	dir, err := config.MachineStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFileName), nil
}
