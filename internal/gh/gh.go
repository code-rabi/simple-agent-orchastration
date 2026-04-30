package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/config"
)

const issueLimit = 100

type Repository struct {
	Owner     string
	Name      string
	Host      string
	Slug      string
	RemoteURL string
	LocalPath string
}

type Issue struct {
	Number    int
	Title     string
	Body      string
	URL       string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Labels    []string
	Assignees []string
}

type DeliveryResult struct {
	Branch         string
	WorktreePath   string
	CommitSHA      string
	PullRequestURL string
	HasChanges     bool
}

type TaskWorktree struct {
	Name       string
	BaseBranch string
	Path       string
}

type issueJSON struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

func DetectRepository(ctx context.Context, repoPath string) (Repository, error) {
	repoPath = filepath.Clean(repoPath)
	remoteURL, err := gitOutput(ctx, repoPath, "remote", "get-url", "origin")
	if err != nil {
		return Repository{}, fmt.Errorf("resolve git origin for %s: %w", repoPath, err)
	}
	repo, err := parseRemote(strings.TrimSpace(remoteURL))
	if err != nil {
		return Repository{}, err
	}
	repo.RemoteURL = strings.TrimSpace(remoteURL)
	repo.LocalPath = repoPath
	return repo, nil
}

func ListIssues(ctx context.Context, repo Repository, repoCfg config.RepoConfig) ([]Issue, error) {
	var all []Issue
	seen := map[string]struct{}{}

	for _, source := range repoCfg.Selection.Sources {
		if source.Type != "issue" {
			continue
		}

		args := []string{
			"issue", "list",
			"--repo", repo.Slug,
			"--limit", fmt.Sprintf("%d", issueLimit),
			"--json", "number,title,body,url,state,createdAt,updatedAt,labels,assignees",
		}
		if source.Filters.State != "" {
			args = append(args, "--state", source.Filters.State)
		}
		for _, label := range source.Filters.Labels {
			args = append(args, "--label", label)
		}
		if source.Filters.Assignee != "" && source.Filters.Assignee != "unassigned" {
			args = append(args, "--assignee", source.Filters.Assignee)
		}
		if source.Filters.Assignee == "unassigned" {
			search := []string{"no:assignee"}
			if source.Filters.State != "" && source.Filters.State != "all" {
				search = append(search, "state:"+source.Filters.State)
			}
			args = append(args, "--search", strings.Join(search, " "))
		}

		out, err := ghJSON(ctx, "", args...)
		if err != nil {
			return nil, fmt.Errorf("list issues for %s: %w", repo.Slug, err)
		}

		var raw []issueJSON
		if err := json.Unmarshal(out, &raw); err != nil {
			return nil, fmt.Errorf("decode gh issues for %s: %w", repo.Slug, err)
		}

		for _, item := range raw {
			issue, err := mapIssue(item)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[issue.URL]; ok {
				continue
			}
			seen[issue.URL] = struct{}{}
			all = append(all, issue)
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Number < all[j].Number
	})
	return all, nil
}

func PrepareTaskWorktree(ctx context.Context, repo Repository, issue Issue) (TaskWorktree, error) {
	repoPath := repo.LocalPath
	baseBranch, err := gitOutput(ctx, repoPath, "branch", "--show-current")
	if err != nil {
		return TaskWorktree{}, fmt.Errorf("read current branch: %w", err)
	}
	baseBranch = strings.TrimSpace(baseBranch)
	if baseBranch == "" {
		return TaskWorktree{}, errors.New("cannot dispatch from detached HEAD")
	}

	stateDir, err := config.MachineStateDir()
	if err != nil {
		return TaskWorktree{}, err
	}

	suffix := time.Now().UTC().Format("20060102-150405")
	branch := fmt.Sprintf("sao/issue-%d-%s", issue.Number, suffix)
	worktreePath := filepath.Join(stateDir, "worktrees", sanitizePathComponent(repo.Slug), fmt.Sprintf("issue-%d-%s", issue.Number, suffix))
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return TaskWorktree{}, fmt.Errorf("create worktree parent: %w", err)
	}

	if _, err := gitOutput(ctx, repoPath, "worktree", "add", "-b", branch, worktreePath, "HEAD"); err != nil {
		return TaskWorktree{}, fmt.Errorf("create task worktree %s: %w", worktreePath, err)
	}
	return TaskWorktree{Name: branch, BaseBranch: baseBranch, Path: worktreePath}, nil
}

func PublishTaskChanges(ctx context.Context, repo Repository, issue Issue, worktree TaskWorktree, agentSummary string) (DeliveryResult, error) {
	clean, err := WorkingTreeClean(ctx, worktree.Path)
	if err != nil {
		return DeliveryResult{}, err
	}

	result := DeliveryResult{
		Branch:       worktree.Name,
		WorktreePath: worktree.Path,
		HasChanges:   !clean,
	}
	if clean {
		return result, nil
	}

	if _, err := gitOutput(ctx, worktree.Path, "add", "-A"); err != nil {
		return DeliveryResult{}, fmt.Errorf("stage changes: %w", err)
	}

	commitTitle := fmt.Sprintf("Fix #%d: %s", issue.Number, issue.Title)
	commitBody := strings.TrimSpace(fmt.Sprintf("Implemented by sao.\n\nIssue: %s\n\nAgent summary:\n%s", issue.URL, strings.TrimSpace(agentSummary)))
	if _, err := gitOutput(ctx, worktree.Path, "commit", "-m", commitTitle, "-m", commitBody); err != nil {
		return DeliveryResult{}, fmt.Errorf("commit task changes: %w", err)
	}

	commitSHA, err := gitOutput(ctx, worktree.Path, "rev-parse", "--short", "HEAD")
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("read commit sha: %w", err)
	}
	result.CommitSHA = strings.TrimSpace(commitSHA)

	if _, err := gitOutput(ctx, worktree.Path, "push", "-u", "origin", worktree.Name); err != nil {
		return DeliveryResult{}, fmt.Errorf("push task branch: %w", err)
	}

	prURL, err := createDraftPR(ctx, repo, issue, worktree.Name, agentSummary)
	if err != nil {
		return DeliveryResult{}, err
	}
	result.PullRequestURL = prURL
	return result, nil
}

func DeliveryForWorktree(worktree TaskWorktree) DeliveryResult {
	return DeliveryResult{
		Branch:       worktree.Name,
		WorktreePath: worktree.Path,
	}
}

func WorkingTreeClean(ctx context.Context, repoPath string) (bool, error) {
	out, err := gitOutput(ctx, repoPath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("check working tree: %w", err)
	}
	return strings.TrimSpace(out) == "", nil
}

func gitOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return string(out), nil
}

func createDraftPR(ctx context.Context, repo Repository, issue Issue, branch, agentSummary string) (string, error) {
	title := fmt.Sprintf("Fix #%d: %s", issue.Number, issue.Title)
	body := strings.TrimSpace(fmt.Sprintf("Closes %s\n\nAgent summary:\n%s", issue.URL, strings.TrimSpace(agentSummary)))
	if body == "" {
		body = fmt.Sprintf("Closes %s", issue.URL)
	}
	out, err := ghOutput(
		ctx,
		repo.LocalPath,
		"pr", "create",
		"--repo", repo.Slug,
		"--draft",
		"--head", branch,
		"--title", title,
		"--body", body,
	)
	if err != nil {
		return "", fmt.Errorf("create draft pull request: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func sanitizePathComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func ghJSON(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	out, err := ghOutput(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func ghOutput(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return string(out), nil
}

func mapIssue(item issueJSON) (Issue, error) {
	createdAt, err := time.Parse(time.RFC3339, item.CreatedAt)
	if err != nil {
		return Issue{}, fmt.Errorf("parse createdAt for issue %d: %w", item.Number, err)
	}
	updatedAt, err := time.Parse(time.RFC3339, item.UpdatedAt)
	if err != nil {
		return Issue{}, fmt.Errorf("parse updatedAt for issue %d: %w", item.Number, err)
	}

	issue := Issue{
		Number:    item.Number,
		Title:     item.Title,
		Body:      item.Body,
		URL:       item.URL,
		State:     item.State,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	for _, label := range item.Labels {
		issue.Labels = append(issue.Labels, label.Name)
	}
	for _, assignee := range item.Assignees {
		issue.Assignees = append(issue.Assignees, assignee.Login)
	}
	return issue, nil
}

func parseRemote(remote string) (Repository, error) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@"):
		parts := strings.SplitN(strings.TrimPrefix(remote, "git@"), ":", 2)
		if len(parts) != 2 {
			return Repository{}, fmt.Errorf("unsupported git remote %q", remote)
		}
		host := parts[0]
		pathParts := strings.Split(strings.Trim(parts[1], "/"), "/")
		if len(pathParts) < 2 {
			return Repository{}, fmt.Errorf("unsupported git remote %q", remote)
		}
		owner := pathParts[len(pathParts)-2]
		name := pathParts[len(pathParts)-1]
		return Repository{
			Host:  host,
			Owner: owner,
			Name:  name,
			Slug:  owner + "/" + name,
		}, nil
	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"):
		trimmed := strings.TrimPrefix(strings.TrimPrefix(remote, "https://"), "http://")
		parts := strings.SplitN(trimmed, "/", 3)
		if len(parts) < 3 {
			return Repository{}, fmt.Errorf("unsupported git remote %q", remote)
		}
		host := parts[0]
		owner := parts[1]
		name := parts[2]
		return Repository{
			Host:  host,
			Owner: owner,
			Name:  name,
			Slug:  owner + "/" + name,
		}, nil
	default:
		return Repository{}, fmt.Errorf("unsupported git remote %q", remote)
	}
}
