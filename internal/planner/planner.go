package planner

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/config"
	"github.com/nitayr/simple-agent-orchastration/internal/gh"
)

type ProjectPlan struct {
	Project config.ProjectRef
	Repo    gh.Repository
	Config  config.RepoConfig
	Issues  []gh.Issue
}

type Candidate struct {
	ProjectPath string
	Repo        gh.Repository
	RepoConfig  config.RepoConfig
	Issue       gh.Issue
	Score       int
	AgentOrder  []string
}

func BuildProjectPlans(ctx context.Context, machineCfg config.MachineConfig) ([]ProjectPlan, error) {
	var plans []ProjectPlan
	for _, project := range machineCfg.Projects {
		if !project.Enabled {
			continue
		}
		repoCfg, err := config.LoadRepoConfig(config.RepoConfigPath(project.Path))
		if err != nil {
			return nil, fmt.Errorf("load repo config for %s: %w", project.Path, err)
		}
		repo, err := gh.DetectRepository(ctx, project.Path)
		if err != nil {
			return nil, fmt.Errorf("detect repository for %s: %w", project.Path, err)
		}
		issues, err := gh.ListIssues(ctx, repo, repoCfg)
		if err != nil {
			return nil, fmt.Errorf("list issues for %s: %w", project.Path, err)
		}
		plans = append(plans, ProjectPlan{
			Project: project,
			Repo:    repo,
			Config:  repoCfg,
			Issues:  issues,
		})
	}
	return plans, nil
}

func RankCandidates(machineCfg config.MachineConfig, projects []ProjectPlan) []Candidate {
	var candidates []Candidate
	for _, project := range projects {
		order := mergedAgentOrder(machineCfg, project.Config)
		for _, issue := range project.Issues {
			candidates = append(candidates, Candidate{
				ProjectPath: project.Project.Path,
				Repo:        project.Repo,
				RepoConfig:  project.Config,
				Issue:       issue,
				Score:       scoreIssue(project.Config, issue, time.Now().UTC()),
				AgentOrder:  order,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if !candidates[i].Issue.CreatedAt.Equal(candidates[j].Issue.CreatedAt) {
			return candidates[i].Issue.CreatedAt.Before(candidates[j].Issue.CreatedAt)
		}
		if candidates[i].Repo.Slug != candidates[j].Repo.Slug {
			return candidates[i].Repo.Slug < candidates[j].Repo.Slug
		}
		return candidates[i].Issue.Number < candidates[j].Issue.Number
	})
	return candidates
}

func scoreIssue(repoCfg config.RepoConfig, issue gh.Issue, now time.Time) int {
	score := 0
	for _, label := range issue.Labels {
		if value, ok := repoCfg.Priority.Labels[label]; ok && value > score {
			score = value
		}
	}

	ageDays := int(now.Sub(issue.CreatedAt).Hours() / 24)
	if ageDays > 0 {
		score += min(ageDays, 30)
	}
	return score
}

func mergedAgentOrder(machineCfg config.MachineConfig, repoCfg config.RepoConfig) []string {
	enabled := map[string]struct{}{}
	for _, agent := range machineCfg.Agents.Installed {
		if agent.Enabled {
			enabled[agent.Name] = struct{}{}
		}
	}

	var order []string
	seen := map[string]struct{}{}
	appendNames := func(names []string) {
		for _, name := range names {
			if _, ok := enabled[name]; !ok {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			order = append(order, name)
		}
	}

	appendNames(repoCfg.Routing.PreferredOrder)
	appendNames(machineCfg.Agents.DefaultOrder)
	return order
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
