package sao

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/config"
	"github.com/nitayr/simple-agent-orchastration/internal/gh"
	"github.com/nitayr/simple-agent-orchastration/internal/planner"
	"github.com/nitayr/simple-agent-orchastration/internal/state"
)

func TestInitProjectCreatesRepoConfigAndRegistersProject(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init error = %v\n%s", err, output)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config"))
	t.Setenv("HOME", tmpDir)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"init-project"}, &stdout, io.Discard); err != nil {
		t.Fatalf("Run(init-project) error = %v", err)
	}

	if _, err := os.Stat(config.RepoConfigPath(repoDir)); err != nil {
		t.Fatalf("repo config was not created: %v", err)
	}

	machinePath, err := config.MachineConfigPath()
	if err != nil {
		t.Fatalf("MachineConfigPath() error = %v", err)
	}
	cfg, err := config.LoadMachineConfig(machinePath)
	if err != nil {
		t.Fatalf("LoadMachineConfig() error = %v", err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("len(cfg.Projects) = %d, want 1", len(cfg.Projects))
	}
	if cfg.Projects[0].Path != repoDir {
		t.Fatalf("cfg.Projects[0].Path = %q, want %q", cfg.Projects[0].Path, repoDir)
	}
	if !cfg.Projects[0].Enabled {
		t.Fatal("cfg.Projects[0].Enabled = false, want true")
	}
}

func TestSelectDispatchPlansHonorsLimits(t *testing.T) {
	t.Parallel()

	cfg := config.MachineConfig{
		Runtime: config.MachineRuntime{
			MaxConcurrentTasks: 2,
		},
		Agents: config.MachineAgents{
			DefaultOrder: []string{"codex", "claude"},
			Installed: []config.InstalledAgent{
				{
					Name:        "codex",
					Type:        "codex",
					Command:     []string{"sh"},
					Enabled:     true,
					MaxParallel: 1,
				},
				{
					Name:        "claude",
					Type:        "claude",
					Command:     []string{"sh"},
					Enabled:     true,
					MaxParallel: 1,
				},
			},
		},
	}

	now := time.Now().UTC()
	candidates := []planner.Candidate{
		{
			ProjectPath: "/tmp/repo-a",
			Repo:        gh.Repository{Slug: "org/repo-a"},
			Issue: gh.Issue{
				Number:    1,
				URL:       "https://example.com/a/1",
				UpdatedAt: now,
			},
			AgentOrder: []string{"codex", "claude"},
		},
		{
			ProjectPath: "/tmp/repo-b",
			Repo:        gh.Repository{Slug: "org/repo-b"},
			Issue: gh.Issue{
				Number:    2,
				URL:       "https://example.com/b/2",
				UpdatedAt: now,
			},
			AgentOrder: []string{"codex", "claude"},
		},
		{
			ProjectPath: "/tmp/repo-c",
			Repo:        gh.Repository{Slug: "org/repo-c"},
			Issue: gh.Issue{
				Number:    3,
				URL:       "https://example.com/c/3",
				UpdatedAt: now,
			},
			AgentOrder: []string{"codex", "claude"},
		},
	}

	plans, err := selectDispatchPlans(cfg, candidates, state.Store{Tasks: map[string]state.TaskRecord{}})
	if err != nil {
		t.Fatalf("selectDispatchPlans() error = %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("len(plans) = %d, want 2", len(plans))
	}
	if plans[0].Agent.Name != "codex" {
		t.Fatalf("plans[0].Agent.Name = %q, want %q", plans[0].Agent.Name, "codex")
	}
	if plans[1].Agent.Name != "claude" {
		t.Fatalf("plans[1].Agent.Name = %q, want %q", plans[1].Agent.Name, "claude")
	}
}

func TestSelectDispatchPlansAvoidsSameRepoParallelism(t *testing.T) {
	t.Parallel()

	cfg := config.MachineConfig{
		Runtime: config.MachineRuntime{
			MaxConcurrentTasks: 2,
		},
		Agents: config.MachineAgents{
			DefaultOrder: []string{"codex"},
			Installed: []config.InstalledAgent{
				{
					Name:        "codex",
					Type:        "codex",
					Command:     []string{"sh"},
					Enabled:     true,
					MaxParallel: 2,
				},
			},
		},
	}

	now := time.Now().UTC()
	candidates := []planner.Candidate{
		{
			ProjectPath: "/tmp/repo-a",
			Repo:        gh.Repository{Slug: "org/repo-a"},
			Issue: gh.Issue{
				Number:    1,
				URL:       "https://example.com/a/1",
				UpdatedAt: now,
			},
			AgentOrder: []string{"codex"},
		},
		{
			ProjectPath: "/tmp/repo-a",
			Repo:        gh.Repository{Slug: "org/repo-a"},
			Issue: gh.Issue{
				Number:    2,
				URL:       "https://example.com/a/2",
				UpdatedAt: now,
			},
			AgentOrder: []string{"codex"},
		},
		{
			ProjectPath: "/tmp/repo-b",
			Repo:        gh.Repository{Slug: "org/repo-b"},
			Issue: gh.Issue{
				Number:    3,
				URL:       "https://example.com/b/3",
				UpdatedAt: now,
			},
			AgentOrder: []string{"codex"},
		},
	}

	plans, err := selectDispatchPlans(cfg, candidates, state.Store{Tasks: map[string]state.TaskRecord{}})
	if err != nil {
		t.Fatalf("selectDispatchPlans() error = %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("len(plans) = %d, want 2", len(plans))
	}
	if plans[0].Candidate.ProjectPath == plans[1].Candidate.ProjectPath {
		t.Fatalf("selected two plans for the same repo path: %q", plans[0].Candidate.ProjectPath)
	}
}
