package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// StringSlice unmarshals from either a YAML string (any block scalar: |, >, |-,
// >-) treated as a single element, or a YAML sequence of strings.
type StringSlice []string

func (s *StringSlice) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Value == "" {
			*s = nil
			return nil
		}
		*s = StringSlice{value.Value}
		return nil
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		*s = items
		return nil
	default:
		return fmt.Errorf("expected string or sequence, got %s", value.Tag)
	}
}

type SetupConfig struct {
	Branch         string          `yaml:"branch"`
	Tag            string          `yaml:"tag"`
	PipelineSource string          `yaml:"pipeline_source"`
	Docker         *bool           `yaml:"docker"`
	MergeRequest   *MRConfig       `yaml:"merge_request"`
	Upstream       *UpstreamConfig `yaml:"upstream"`
	Schedule       *ScheduleConfig `yaml:"schedule"`
	Chat           *ChatConfig     `yaml:"chat"`
	Git            *GitSetupConfig `yaml:"git"`
	API            *APISetupConfig `yaml:"api"`
	Mocks          *MocksConfig    `yaml:"mocks"`
}

type MRConfig struct {
	Title        string `yaml:"title"`
	TargetBranch string `yaml:"target_branch"`
	IID          int    `yaml:"iid"`
	Draft        bool   `yaml:"draft"`
	Labels       string `yaml:"labels"`
	Assignees    string `yaml:"assignees"`
}

type UpstreamConfig struct {
	PipelineID int `yaml:"pipeline_id"`
	ProjectID  int `yaml:"project_id"`
	JobID      int `yaml:"job_id"`
}

type ScheduleConfig struct {
	Description string `yaml:"description"`
}

type ChatConfig struct {
	Channel string `yaml:"channel"`
	Input   string `yaml:"input"`
	UserID  string `yaml:"user_id"`
}

type GitSetupConfig struct {
	User   GitUserConfig    `yaml:"user"`
	Origin *GitOriginConfig `yaml:"origin"`
}

type GitUserConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type GitOriginConfig struct {
	Branch   string            `yaml:"branch"`
	Files    map[string]string `yaml:"files"`
	Commands StringSlice       `yaml:"commands"`
}

type APISetupConfig struct {
	Token   *TokenConfig   `yaml:"token"`
	Project *ProjectConfig `yaml:"project"`
	Seed    *APISeedConfig `yaml:"seed"`
}

type TokenConfig struct {
	Valid     bool        `yaml:"valid"`
	ExpiresAt string      `yaml:"expires_at"`
	Scopes    StringSlice `yaml:"scopes"`
}

type ProjectConfig struct {
	DefaultBranch string `yaml:"default_branch"`
	Path          string `yaml:"path"`
}

type APISeedConfig struct {
	Releases      []map[string]interface{} `yaml:"releases"`
	MergeRequests []map[string]interface{} `yaml:"merge_requests"`
	Labels        []map[string]interface{} `yaml:"labels"`
}

type MocksConfig struct {
	Binaries map[string]BinaryMockConfig `yaml:"binaries"`
}

type BinaryMockConfig struct {
	Executable string `yaml:"executable"`
}
