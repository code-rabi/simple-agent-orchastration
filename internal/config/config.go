package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

const (
	RepoConfigName = ".simple-agent-orchestration.yaml"
	MachineAppName = "sao"
)

type MachineConfig struct {
	Runtime  MachineRuntime `yaml:"runtime"`
	Agents   MachineAgents  `yaml:"agents"`
	Projects []ProjectRef   `yaml:"projects"`
}

type MachineRuntime struct {
	MaxConcurrentTasks  int `yaml:"max_concurrent_tasks"`
	PollIntervalSeconds int `yaml:"poll_interval_seconds,omitempty"`
}

type MachineAgents struct {
	DefaultOrder []string         `yaml:"default_order"`
	Installed    []InstalledAgent `yaml:"installed"`
}

type InstalledAgent struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Command     []string `yaml:"command"`
	Enabled     bool     `yaml:"enabled"`
	MaxParallel int      `yaml:"max_parallel,omitempty"`
	Healthcheck []string `yaml:"healthcheck,omitempty"`
}

type ProjectRef struct {
	Path    string `yaml:"path"`
	Enabled bool   `yaml:"enabled"`
}

type RepoConfig struct {
	Version   int           `yaml:"version"`
	Selection RepoSelection `yaml:"selection"`
	Priority  RepoPriority  `yaml:"priority"`
	Routing   RepoRouting   `yaml:"routing"`
	Runtime   RepoRuntime   `yaml:"runtime,omitempty"`
}

type RepoRuntime struct {
	PollIntervalSeconds int `yaml:"poll_interval_seconds,omitempty"`
}

type RepoSelection struct {
	Sources []SelectionSource `yaml:"sources"`
}

type SelectionSource struct {
	Type    string           `yaml:"type"`
	Filters SourceFilterSpec `yaml:"filters"`
}

type SourceFilterSpec struct {
	State    string   `yaml:"state,omitempty"`
	Labels   []string `yaml:"labels,omitempty"`
	Assignee string   `yaml:"assignee,omitempty"`
}

type RepoPriority struct {
	Labels map[string]int `yaml:"labels"`
}

type RepoRouting struct {
	PreferredOrder []string `yaml:"preferred_order,omitempty"`
}

func DefaultMachineConfig() MachineConfig {
	return MachineConfig{
		Runtime: MachineRuntime{
			MaxConcurrentTasks: 2,
		},
		Agents: MachineAgents{
			DefaultOrder: []string{"claude", "codex", "gemini"},
			Installed: []InstalledAgent{
				{
					Name:        "claude",
					Type:        "claude",
					Command:     []string{"claude"},
					Enabled:     true,
					MaxParallel: 1,
					Healthcheck: []string{"which", "claude"},
				},
				{
					Name:        "codex",
					Type:        "codex",
					Command:     []string{"codex"},
					Enabled:     true,
					MaxParallel: 1,
					Healthcheck: []string{"which", "codex"},
				},
				{
					Name:        "gemini",
					Type:        "gemini",
					Command:     []string{"gemini"},
					Enabled:     true,
					MaxParallel: 1,
					Healthcheck: []string{"which", "gemini"},
				},
			},
		},
		Projects: []ProjectRef{},
	}
}

func DefaultRepoConfig() RepoConfig {
	return RepoConfig{
		Version: 1,
		Selection: RepoSelection{
			Sources: []SelectionSource{
				{
					Type: "issue",
					Filters: SourceFilterSpec{
						State:    "open",
						Labels:   []string{"agent-ready"},
						Assignee: "unassigned",
					},
				},
			},
		},
		Priority: RepoPriority{
			Labels: map[string]int{
				"P0": 100,
				"P1": 80,
				"P2": 50,
			},
		},
		Routing: RepoRouting{
			PreferredOrder: []string{"claude", "codex"},
		},
	}
}

func MachineConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, MachineAppName, "config.yaml"), nil
}

func MachineStateDir() (string, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(base, ".local", "state", MachineAppName), nil
}

func RepoConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, RepoConfigName)
}

func LoadMachineConfig(path string) (MachineConfig, error) {
	var cfg MachineConfig
	if err := loadYAML(path, &cfg); err != nil {
		return MachineConfig{}, err
	}
	applyMachineDefaults(&cfg)
	return cfg, nil
}

func SaveMachineConfig(path string, cfg MachineConfig) error {
	applyMachineDefaults(&cfg)
	if err := ValidateMachineConfig(cfg); err != nil {
		return err
	}
	return saveYAML(path, cfg)
}

func LoadRepoConfig(path string) (RepoConfig, error) {
	var cfg RepoConfig
	if err := loadYAML(path, &cfg); err != nil {
		return RepoConfig{}, err
	}
	applyRepoDefaults(&cfg)
	return cfg, nil
}

func SaveRepoConfig(path string, cfg RepoConfig) error {
	applyRepoDefaults(&cfg)
	if err := ValidateRepoConfig(cfg); err != nil {
		return err
	}
	return saveYAML(path, cfg)
}

func ValidateMachineConfig(cfg MachineConfig) error {
	if cfg.Runtime.MaxConcurrentTasks <= 0 {
		return errors.New("runtime.max_concurrent_tasks must be greater than 0")
	}
	if len(cfg.Agents.Installed) == 0 {
		return errors.New("agents.installed must contain at least one agent")
	}
	seen := map[string]struct{}{}
	for _, agent := range cfg.Agents.Installed {
		if agent.Name == "" {
			return errors.New("agents.installed[].name is required")
		}
		if _, ok := seen[agent.Name]; ok {
			return fmt.Errorf("duplicate agent name %q", agent.Name)
		}
		seen[agent.Name] = struct{}{}
		if len(agent.Command) == 0 {
			return fmt.Errorf("agent %q must define command", agent.Name)
		}
		if agent.MaxParallel < 0 {
			return fmt.Errorf("agent %q max_parallel must be >= 0", agent.Name)
		}
	}
	for _, name := range cfg.Agents.DefaultOrder {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("agents.default_order references unknown agent %q", name)
		}
	}
	for _, project := range cfg.Projects {
		if project.Path == "" {
			return errors.New("projects[].path is required")
		}
	}
	return nil
}

func ValidateRepoConfig(cfg RepoConfig) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported repo config version %d", cfg.Version)
	}
	if len(cfg.Selection.Sources) == 0 {
		return errors.New("selection.sources must contain at least one source")
	}
	for _, source := range cfg.Selection.Sources {
		if source.Type == "" {
			return errors.New("selection.sources[].type is required")
		}
	}
	if len(cfg.Priority.Labels) == 0 {
		return errors.New("priority.labels must contain at least one label")
	}
	return nil
}

func AddProject(cfg MachineConfig, projectPath string) MachineConfig {
	projectPath = filepath.Clean(projectPath)
	idx := slices.IndexFunc(cfg.Projects, func(project ProjectRef) bool {
		return filepath.Clean(project.Path) == projectPath
	})
	if idx >= 0 {
		cfg.Projects[idx].Enabled = true
		cfg.Projects[idx].Path = projectPath
		return cfg
	}
	cfg.Projects = append(cfg.Projects, ProjectRef{
		Path:    projectPath,
		Enabled: true,
	})
	return cfg
}

func applyMachineDefaults(cfg *MachineConfig) {
	if cfg.Runtime.MaxConcurrentTasks == 0 {
		cfg.Runtime.MaxConcurrentTasks = 2
	}
	if len(cfg.Agents.DefaultOrder) == 0 {
		defaults := DefaultMachineConfig()
		cfg.Agents.DefaultOrder = defaults.Agents.DefaultOrder
	}
	if len(cfg.Agents.Installed) == 0 {
		defaults := DefaultMachineConfig()
		cfg.Agents.Installed = defaults.Agents.Installed
	}
}

func applyRepoDefaults(cfg *RepoConfig) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if len(cfg.Selection.Sources) == 0 {
		defaults := DefaultRepoConfig()
		cfg.Selection = defaults.Selection
	}
	if len(cfg.Priority.Labels) == 0 {
		defaults := DefaultRepoConfig()
		cfg.Priority = defaults.Priority
	}
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func saveYAML(path string, in any) error {
	data, err := yaml.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal yaml for %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
