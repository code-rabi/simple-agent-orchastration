package sao

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nitayr/simple-agent-orchastration/internal/acpx"
	"github.com/nitayr/simple-agent-orchastration/internal/config"
	"github.com/nitayr/simple-agent-orchastration/internal/gh"
	"github.com/nitayr/simple-agent-orchastration/internal/planner"
	"github.com/nitayr/simple-agent-orchastration/internal/state"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "run":
		return runLoop(ctx, stdout, stderr)
	case "once":
		return runOnce(ctx, stdout, stderr)
	case "init-machine":
		return initMachine(stdout)
	case "init-project":
		return initProject(stdout)
	case "init-repo":
		return initRepo(stdout)
	case "add-repo":
		return addRepo(args, stdout)
	case "update":
		return update(ctx, stdout, stderr)
	case "validate":
		return validate(stdout, stderr)
	case "agents":
		return listAgents(stdout)
	case "plan":
		return showPlan(stdout)
	case "-h", "--help", "help":
		printHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "sao")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  sao                Run the foreground orchestration loop")
	fmt.Fprintln(w, "  sao once           Run a single orchestration cycle")
	fmt.Fprintln(w, "  sao init-machine   Create ~/.config/sao/config.yaml")
	fmt.Fprintln(w, "  sao init-project   Create repo config and register current repo")
	fmt.Fprintln(w, "  sao init-repo      Create .simple-agent-orchestration.yaml in the current repo")
	fmt.Fprintln(w, "  sao add-repo PATH  Register a repo in machine config")
	fmt.Fprintln(w, "  sao update         Install the latest released sao binary")
	fmt.Fprintln(w, "  sao validate       Validate configs, git remotes, gh auth, and agent prerequisites")
	fmt.Fprintln(w, "  sao agents         Show configured agents")
	fmt.Fprintln(w, "  sao plan           Show ranked candidate tasks without dispatching")
}

func runLoop(ctx context.Context, stdout, stderr io.Writer) error {
	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadMachineConfig(machinePath)
	if err != nil {
		return fmt.Errorf("load machine config before run: %w", err)
	}

	interval := effectivePollInterval(cfg)
	fmt.Fprintln(stdout, "sao foreground loop")
	fmt.Fprintf(stdout, "machine config: %s\n", machinePath)
	fmt.Fprintf(stdout, "poll interval: %s\n", interval)

	if err := runCycle(ctx, cfg, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "cycle error: %v\n", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(stdout, "sao loop stopped")
			return nil
		case <-ticker.C:
			cfg, err := config.LoadMachineConfig(machinePath)
			if err != nil {
				fmt.Fprintf(stderr, "reload machine config failed: %v\n", err)
				continue
			}
			if err := runCycle(ctx, cfg, stdout, stderr); err != nil {
				fmt.Fprintf(stderr, "cycle error: %v\n", err)
			}
		}
	}
}

func runOnce(ctx context.Context, stdout, stderr io.Writer) error {
	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadMachineConfig(machinePath)
	if err != nil {
		return fmt.Errorf("load machine config before run: %w", err)
	}
	return runCycle(ctx, cfg, stdout, stderr)
}

func initMachine(stdout io.Writer) error {
	path, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(stdout, "machine config already exists: %s\n", path)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg := config.DefaultMachineConfig()
	if err := config.SaveMachineConfig(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created machine config: %s\n", path)
	return nil
}

func initRepo(stdout io.Writer) error {
	repoRoot, err := resolveRepoRoot(".")
	if err != nil {
		return err
	}
	created, err := ensureRepoConfig(repoRoot)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(stdout, "created repo config: %s\n", config.RepoConfigPath(repoRoot))
	} else {
		fmt.Fprintf(stdout, "repo config already exists: %s\n", config.RepoConfigPath(repoRoot))
	}
	return nil
}

func initProject(stdout io.Writer) error {
	repoRoot, err := resolveRepoRoot(".")
	if err != nil {
		return err
	}

	createdRepoConfig, err := ensureRepoConfig(repoRoot)
	if err != nil {
		return err
	}
	if createdRepoConfig {
		fmt.Fprintf(stdout, "created repo config: %s\n", config.RepoConfigPath(repoRoot))
	} else {
		fmt.Fprintf(stdout, "repo config already exists: %s\n", config.RepoConfigPath(repoRoot))
	}

	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	cfg, err := ensureMachineConfig(machinePath)
	if err != nil {
		return err
	}
	cfg = config.AddProject(cfg, repoRoot)
	if err := config.SaveMachineConfig(machinePath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "registered repo: %s\n", repoRoot)
	return nil
}

func ensureRepoConfig(repoRoot string) (bool, error) {
	path := config.RepoConfigPath(repoRoot)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	cfg := config.DefaultRepoConfig()
	if err := config.SaveRepoConfig(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

func addRepo(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: sao add-repo /path/to/repo")
	}
	repoRoot, err := resolveRepoRoot(args[0])
	if err != nil {
		return err
	}
	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	cfg, err := ensureMachineConfig(machinePath)
	if err != nil {
		return err
	}
	cfg = config.AddProject(cfg, repoRoot)
	if err := config.SaveMachineConfig(machinePath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "registered repo: %s\n", repoRoot)
	return nil
}

func update(ctx context.Context, stdout, stderr io.Writer) error {
	if _, err := exec.LookPath("bash"); err != nil {
		return errors.New("required CLI missing: bash")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		return errors.New("required CLI missing: curl")
	}

	repo := os.Getenv("SAO_REPO")
	if repo == "" {
		repo = "code-rabi/simple-agent-orchastration"
	}
	installerURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/install.sh", repo)

	cmd := exec.CommandContext(ctx, "bash", "-c", `set -euo pipefail; curl -fsSL "$SAO_INSTALL_URL" | bash`)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), "SAO_INSTALL_URL="+installerURL)
	if os.Getenv("SAO_INSTALL_DIR") == "" {
		if installDir, ok := currentInstallDir(); ok {
			cmd.Env = append(cmd.Env, "SAO_INSTALL_DIR="+installDir)
		}
	}

	fmt.Fprintf(stdout, "updating sao from %s\n", installerURL)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	return nil
}

func validate(stdout, stderr io.Writer) error {
	var problems []string

	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	machineCfg, err := config.LoadMachineConfig(machinePath)
	if err != nil {
		problems = append(problems, fmt.Sprintf("machine config: %v", err))
	} else if err := config.ValidateMachineConfig(machineCfg); err != nil {
		problems = append(problems, fmt.Sprintf("machine config invalid: %v", err))
	} else {
		fmt.Fprintf(stdout, "ok machine config: %s\n", machinePath)
	}

	if _, err := exec.LookPath("gh"); err != nil {
		problems = append(problems, "required CLI missing: gh")
	} else {
		fmt.Fprintln(stdout, "ok dependency: gh")
		if err := exec.Command("gh", "auth", "status").Run(); err != nil {
			problems = append(problems, "gh authentication is not ready")
		} else {
			fmt.Fprintln(stdout, "ok gh auth")
		}
	}

	for _, agent := range machineCfg.Agents.Installed {
		if !agent.Enabled || len(agent.Command) == 0 {
			continue
		}
		if _, err := exec.LookPath(agent.Command[0]); err != nil {
			problems = append(problems, fmt.Sprintf("configured agent missing from PATH: %s", agent.Name))
			continue
		}
		if _, ok := runtimeAgentName(agent); !ok {
			problems = append(problems, fmt.Sprintf("configured agent is not supported for direct execution: %s", agent.Name))
			continue
		}
		fmt.Fprintf(stdout, "ok agent runtime: %s\n", agent.Name)
	}

	for _, project := range machineCfg.Projects {
		repoPath := filepath.Clean(project.Path)
		repoCfgPath := config.RepoConfigPath(repoPath)
		repoCfg, err := config.LoadRepoConfig(repoCfgPath)
		if err != nil {
			problems = append(problems, fmt.Sprintf("repo config for %s: %v", repoPath, err))
			continue
		}
		if err := config.ValidateRepoConfig(repoCfg); err != nil {
			problems = append(problems, fmt.Sprintf("repo config invalid for %s: %v", repoPath, err))
			continue
		}
		repo, err := gh.DetectRepository(context.Background(), repoPath)
		if err != nil {
			problems = append(problems, fmt.Sprintf("git remote detection for %s: %v", repoPath, err))
			continue
		}
		fmt.Fprintf(stdout, "ok repo config: %s\n", repoCfgPath)
		fmt.Fprintf(stdout, "ok git remote: %s -> %s\n", repoPath, repo.Slug)
	}

	if _, err := state.Load(); err != nil {
		problems = append(problems, fmt.Sprintf("local state: %v", err))
	} else if statePath, pathErr := state.Path(); pathErr == nil {
		fmt.Fprintf(stdout, "ok state path: %s\n", statePath)
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	fmt.Fprintln(stdout, "validation passed")
	return nil
}

func listAgents(stdout io.Writer) error {
	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadMachineConfig(machinePath)
	if err != nil {
		return err
	}
	for _, agent := range cfg.Agents.Installed {
		fmt.Fprintf(stdout, "%s enabled=%t command=%s\n", agent.Name, agent.Enabled, strings.Join(agent.Command, " "))
	}
	return nil
}

func showPlan(stdout io.Writer) error {
	cfg, err := loadMachineConfig()
	if err != nil {
		return err
	}
	projectPlans, err := planner.BuildProjectPlans(context.Background(), cfg)
	if err != nil {
		return err
	}
	candidates := planner.RankCandidates(cfg, projectPlans)
	fmt.Fprintf(stdout, "registered projects: %d\n", len(projectPlans))
	fmt.Fprintf(stdout, "candidates: %d\n", len(candidates))
	for idx, candidate := range candidates {
		fmt.Fprintf(
			stdout,
			"%d. [%s] #%d score=%d agents=%s %s\n",
			idx+1,
			candidate.Repo.Slug,
			candidate.Issue.Number,
			candidate.Score,
			strings.Join(candidate.AgentOrder, ","),
			candidate.Issue.Title,
		)
		if idx >= 9 {
			break
		}
	}
	return nil
}

func ensureMachineConfig(path string) (config.MachineConfig, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		cfg := config.DefaultMachineConfig()
		if err := config.SaveMachineConfig(path, cfg); err != nil {
			return config.MachineConfig{}, err
		}
		return cfg, nil
	} else if err != nil {
		return config.MachineConfig{}, err
	}
	return config.LoadMachineConfig(path)
}

func resolveRepoRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	cmd := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("path is not a git repository: %s", abs)
	}
	return strings.TrimSpace(string(out)), nil
}

func loadMachineConfig() (config.MachineConfig, error) {
	machinePath, err := config.MachineConfigPath()
	if err != nil {
		return config.MachineConfig{}, err
	}
	return config.LoadMachineConfig(machinePath)
}

func currentInstallDir() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", false
	}
	switch filepath.Base(executable) {
	case "sao", "sao.exe":
	default:
		return "", false
	}

	dir := filepath.Dir(executable)
	if !dirIsWritable(dir) {
		return "", false
	}
	return dir, true
}

func dirIsWritable(dir string) bool {
	file, err := os.CreateTemp(dir, ".sao-write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(name)
		return false
	}
	return os.Remove(name) == nil
}

func effectivePollInterval(cfg config.MachineConfig) time.Duration {
	seconds := cfg.Runtime.PollIntervalSeconds
	if seconds <= 0 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func runCycle(ctx context.Context, cfg config.MachineConfig, stdout, stderr io.Writer) error {
	projectPlans, err := planner.BuildProjectPlans(ctx, cfg)
	if err != nil {
		return err
	}
	candidates := planner.RankCandidates(cfg, projectPlans)
	fmt.Fprintf(stdout, "[%s] discovered %d candidate tasks across %d repos\n", time.Now().Format(time.RFC3339), len(candidates), len(projectPlans))
	if len(candidates) == 0 {
		return nil
	}

	store, err := state.Load()
	if err != nil {
		return err
	}

	plans, err := selectDispatchPlans(cfg, candidates, store)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		fmt.Fprintln(stdout, "no eligible tasks after state and concurrency filtering")
		return nil
	}

	now := time.Now().UTC()
	for _, plan := range plans {
		fmt.Fprintf(stdout, "dispatching %s #%d with %s\n", plan.Candidate.Repo.Slug, plan.Candidate.Issue.Number, plan.Agent.Name)
		store.Tasks[plan.Candidate.Issue.URL] = state.TaskRecord{
			IssueURL:       plan.Candidate.Issue.URL,
			RepoPath:       plan.Candidate.ProjectPath,
			AgentName:      plan.Agent.Name,
			Status:         "running",
			IssueUpdatedAt: plan.Candidate.Issue.UpdatedAt,
			StartedAt:      now,
			UpdatedAt:      now,
		}
	}
	if err := state.Save(store); err != nil {
		return err
	}

	outcomes := make(chan dispatchOutcome, len(plans))
	var wg sync.WaitGroup
	for _, plan := range plans {
		wg.Add(1)
		go func(plan dispatchPlan) {
			defer wg.Done()
			worktree, err := gh.PrepareTaskWorktree(ctx, plan.Candidate.Repo, plan.Candidate.Issue)
			if err != nil {
				outcomes <- dispatchOutcome{
					Plan:        plan,
					Err:         fmt.Errorf("prepare task worktree: %w", err),
					CompletedAt: time.Now().UTC(),
				}
				return
			}

			prompt := buildPrompt(plan.Candidate, worktree.Path)
			runner := acpx.NewRunner(plan.Agent.Command)
			response, err := runner.Exec(ctx, worktree.Path, plan.RuntimeName, prompt)
			if err != nil {
				outcomes <- dispatchOutcome{
					Plan:        plan,
					Response:    response,
					Delivery:    gh.DeliveryForWorktree(worktree),
					Err:         err,
					CompletedAt: time.Now().UTC(),
				}
				return
			}

			delivery, err := gh.PublishTaskChanges(ctx, plan.Candidate.Repo, plan.Candidate.Issue, worktree, response.AssistantText)
			outcomes <- dispatchOutcome{
				Plan:        plan,
				Response:    response,
				Delivery:    delivery,
				Err:         err,
				CompletedAt: time.Now().UTC(),
			}
		}(plan)
	}
	go func() {
		wg.Wait()
		close(outcomes)
	}()

	var errs []error
	for outcome := range outcomes {
		if outcome.Err != nil {
			markTaskFailure(store, outcome.Plan.Candidate.Issue.URL, outcome.Plan.Candidate.Issue.UpdatedAt, outcome.CompletedAt, outcome.Err, outcome.Delivery)
			fmt.Fprintf(
				stderr,
				"failed %s #%d with %s: %v\n",
				outcome.Plan.Candidate.Repo.Slug,
				outcome.Plan.Candidate.Issue.Number,
				outcome.Plan.Agent.Name,
				outcome.Err,
			)
			errs = append(errs, outcome.Err)
		} else {
			store.Tasks[outcome.Plan.Candidate.Issue.URL] = state.TaskRecord{
				IssueURL:       outcome.Plan.Candidate.Issue.URL,
				RepoPath:       outcome.Plan.Candidate.ProjectPath,
				AgentName:      outcome.Plan.Agent.Name,
				Status:         "completed",
				IssueUpdatedAt: outcome.Plan.Candidate.Issue.UpdatedAt,
				StartedAt:      now,
				UpdatedAt:      outcome.CompletedAt,
				CompletedAt:    outcome.CompletedAt,
				LastResponse:   outcome.Response.AssistantText,
				Branch:         outcome.Delivery.Branch,
				WorktreePath:   outcome.Delivery.WorktreePath,
				CommitSHA:      outcome.Delivery.CommitSHA,
				PullRequestURL: outcome.Delivery.PullRequestURL,
			}
			if outcome.Response.AssistantText != "" {
				fmt.Fprintf(stdout, "completed %s #%d\n", outcome.Plan.Candidate.Repo.Slug, outcome.Plan.Candidate.Issue.Number)
				fmt.Fprintf(stdout, "agent summary:\n%s\n", outcome.Response.AssistantText)
			} else {
				fmt.Fprintf(stdout, "completed %s #%d with no summary returned\n", outcome.Plan.Candidate.Repo.Slug, outcome.Plan.Candidate.Issue.Number)
			}
			if outcome.Delivery.PullRequestURL != "" {
				fmt.Fprintf(stdout, "pull request: %s\n", outcome.Delivery.PullRequestURL)
			} else if outcome.Delivery.Branch != "" && !outcome.Delivery.HasChanges {
				fmt.Fprintf(stdout, "no changes to publish for %s #%d\n", outcome.Plan.Candidate.Repo.Slug, outcome.Plan.Candidate.Issue.Number)
			}
		}

		if err := state.Save(store); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

type dispatchPlan struct {
	Candidate   planner.Candidate
	Agent       config.InstalledAgent
	RuntimeName string
}

type dispatchOutcome struct {
	Plan        dispatchPlan
	Response    acpx.Result
	Delivery    gh.DeliveryResult
	Err         error
	CompletedAt time.Time
}

func chooseAgent(cfg config.MachineConfig, candidate planner.Candidate) (config.InstalledAgent, error) {
	for _, name := range candidate.AgentOrder {
		idx := slices.IndexFunc(cfg.Agents.Installed, func(agent config.InstalledAgent) bool {
			return agent.Name == name && agent.Enabled
		})
		if idx < 0 {
			continue
		}
		agent := cfg.Agents.Installed[idx]
		if _, err := exec.LookPath(agent.Command[0]); err != nil {
			continue
		}
		if _, ok := runtimeAgentName(agent); !ok {
			continue
		}
		return agent, nil
	}
	return config.InstalledAgent{}, fmt.Errorf("no enabled agent available for %s #%d", candidate.Repo.Slug, candidate.Issue.Number)
}

func selectDispatchPlans(cfg config.MachineConfig, candidates []planner.Candidate, store state.Store) ([]dispatchPlan, error) {
	globalLimit := max(cfg.Runtime.MaxConcurrentTasks, 1)
	runningTasks := 0
	runningByAgent := map[string]int{}
	activeRepos := map[string]struct{}{}
	activeIssues := map[string]struct{}{}
	for issueURL, record := range store.Tasks {
		if record.Status != "running" {
			continue
		}
		runningTasks++
		runningByAgent[record.AgentName]++
		if record.RepoPath != "" {
			activeRepos[record.RepoPath] = struct{}{}
		}
		activeIssues[issueURL] = struct{}{}
	}

	availableGlobal := globalLimit - runningTasks
	if availableGlobal <= 0 {
		return nil, nil
	}

	var plans []dispatchPlan
	var unavailableErr error
	for _, candidate := range candidates {
		record, ok := store.Tasks[candidate.Issue.URL]
		if ok && !shouldDispatch(record, candidate) {
			continue
		}
		if _, ok := activeIssues[candidate.Issue.URL]; ok {
			continue
		}
		if _, ok := activeRepos[candidate.ProjectPath]; ok {
			continue
		}

		plan, err := chooseAvailableAgent(cfg, candidate, runningByAgent)
		if err != nil {
			if unavailableErr == nil {
				unavailableErr = err
			}
			continue
		}

		plans = append(plans, plan)
		runningByAgent[plan.Agent.Name]++
		activeRepos[candidate.ProjectPath] = struct{}{}
		activeIssues[candidate.Issue.URL] = struct{}{}
		if len(plans) >= availableGlobal {
			break
		}
	}

	if len(plans) == 0 {
		return nil, unavailableErr
	}
	return plans, nil
}

func chooseAvailableAgent(cfg config.MachineConfig, candidate planner.Candidate, runningByAgent map[string]int) (dispatchPlan, error) {
	for _, name := range candidate.AgentOrder {
		idx := slices.IndexFunc(cfg.Agents.Installed, func(agent config.InstalledAgent) bool {
			return agent.Name == name && agent.Enabled
		})
		if idx < 0 {
			continue
		}
		agent := cfg.Agents.Installed[idx]
		if _, err := exec.LookPath(agent.Command[0]); err != nil {
			continue
		}
		runtimeName, ok := runtimeAgentName(agent)
		if !ok {
			continue
		}
		if runningByAgent[agent.Name] >= effectiveMaxParallel(agent) {
			continue
		}
		return dispatchPlan{
			Candidate:   candidate,
			Agent:       agent,
			RuntimeName: runtimeName,
		}, nil
	}
	return dispatchPlan{}, fmt.Errorf("no enabled agent slot available for %s #%d", candidate.Repo.Slug, candidate.Issue.Number)
}

func effectiveMaxParallel(agent config.InstalledAgent) int {
	if agent.MaxParallel <= 0 {
		return 1
	}
	return agent.MaxParallel
}

func runtimeAgentName(agent config.InstalledAgent) (string, bool) {
	if name, ok := acpx.ResolveAgentName(agent.Type); ok {
		return name, true
	}
	return acpx.ResolveAgentName(agent.Name)
}

func buildPrompt(candidate planner.Candidate, worktreePath string) string {
	body := strings.TrimSpace(candidate.Issue.Body)
	if len(body) > 4000 {
		body = body[:4000] + "\n...[truncated]"
	}

	return strings.TrimSpace(fmt.Sprintf(`
You are working inside a git worktree at: %s
Original watched repository path: %s

GitHub issue:
- Repository: %s
- Number: #%d
- URL: %s
- Title: %s

Issue body:
%s

Please:
1. inspect the repository
2. implement the issue
3. run any relevant local validation if available
4. summarize the changes you made

Do not create a branch, commit, push, or open a pull request. The orchestrator will publish your file changes after you finish.
Do not ask for orchestration help. Work directly in the repository.
`, worktreePath, candidate.ProjectPath, candidate.Repo.Slug, candidate.Issue.Number, candidate.Issue.URL, candidate.Issue.Title, body))
}

func markTaskFailure(store state.Store, issueURL string, issueUpdatedAt, completedAt time.Time, err error, delivery gh.DeliveryResult) {
	record := store.Tasks[issueURL]
	record.Status = "failed"
	record.IssueUpdatedAt = issueUpdatedAt
	record.UpdatedAt = completedAt
	record.CompletedAt = completedAt
	record.LastError = err.Error()
	if delivery.Branch != "" {
		record.Branch = delivery.Branch
	}
	if delivery.WorktreePath != "" {
		record.WorktreePath = delivery.WorktreePath
	}
	store.Tasks[issueURL] = record
}

func shouldDispatch(record state.TaskRecord, candidate planner.Candidate) bool {
	if record.Status == "running" {
		return false
	}
	if record.Status == "failed" {
		return true
	}
	if !record.IssueUpdatedAt.IsZero() && !candidate.Issue.UpdatedAt.After(record.IssueUpdatedAt) {
		return false
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
