package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func ghJSON(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
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
		return nil, errors.New(msg)
	}
	return out, nil
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
