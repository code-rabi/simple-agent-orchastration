package sao

import (
	"testing"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/config"
	"github.com/nitayr/simple-agent-orchastration/internal/gh"
	"github.com/nitayr/simple-agent-orchastration/internal/planner"
	"github.com/nitayr/simple-agent-orchastration/internal/state"
)

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
