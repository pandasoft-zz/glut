package config

import (
	"fmt"
	"strconv"
	"strings"

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
	Branch         string           `yaml:"branch"`
	DefaultBranch  string           `yaml:"default_branch"`
	Tag            string           `yaml:"tag"`
	TagMessage     string           `yaml:"tag_message"`
	PipelineSource string           `yaml:"pipeline_source"`
	Docker         *bool            `yaml:"docker"`
	RefProtected   *bool            `yaml:"ref_protected"`
	MergeRequest   *MRConfig        `yaml:"merge_request"`
	Upstream       *UpstreamConfig  `yaml:"upstream"`
	Schedule       *ScheduleConfig  `yaml:"schedule"`
	Chat           *ChatConfig      `yaml:"chat"`
	Git            *GitSetupConfig  `yaml:"git"`
	Pipeline       *PipelineConfig  `yaml:"pipeline"`
	API            *APISetupConfig  `yaml:"api"`
	Mocks          *MocksConfig     `yaml:"mocks"`
}

type PipelineConfig struct {
	User *PipelineUserConfig `yaml:"user"`
}

type PipelineUserConfig struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
	Login string `yaml:"login"`
}

type MRConfig struct {
	Title        string `yaml:"title"`
	TargetBranch string `yaml:"target_branch"`
	IID          int    `yaml:"iid"`
	Draft        bool   `yaml:"draft"`
	Labels       string `yaml:"labels"`
	Assignees    string `yaml:"assignees"`
	Description  string `yaml:"description"`
	Milestone    string `yaml:"milestone"`
	Squash       bool   `yaml:"squash"`
	Approved     bool   `yaml:"approved"`
	EventType    string `yaml:"event_type"`
	DiffBaseSHA  string `yaml:"diff_base_sha"`
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
	User    *UserConfig    `yaml:"user"`
	Group   *GroupConfig   `yaml:"group"`
}

type UserConfig struct {
	ID    int    `yaml:"id"`
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
	Login string `yaml:"login"`
}

type GroupConfig struct {
	ID   int    `yaml:"id"`
	Path string `yaml:"path"`
	Name string `yaml:"name"`
}

type TokenConfig struct {
	Valid     bool        `yaml:"valid"`
	ExpiresAt string      `yaml:"expires_at"`
	Scopes    StringSlice `yaml:"scopes"`
}

type ProjectConfig struct {
	DefaultBranch string            `yaml:"default_branch"`
	Path          string            `yaml:"path"`
	AccessLevel   *AccessLevelValue `yaml:"access_level"` // nil → 40 (Maintainer)
}

// AccessLevelValue is a GitLab member access level. It accepts either a string
// name ("guest", "reporter", "developer", "maintainer", "owner") or the
// corresponding integer (10, 20, 30, 40, 50) in YAML.
type AccessLevelValue int

const (
	AccessLevelGuest      AccessLevelValue = 10
	AccessLevelReporter   AccessLevelValue = 20
	AccessLevelDeveloper  AccessLevelValue = 30
	AccessLevelMaintainer AccessLevelValue = 40
	AccessLevelOwner      AccessLevelValue = 50
)

func (a *AccessLevelValue) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("access_level must be a string or integer")
	}
	switch strings.ToLower(value.Value) {
	case "guest":
		*a = AccessLevelGuest
	case "reporter":
		*a = AccessLevelReporter
	case "developer":
		*a = AccessLevelDeveloper
	case "maintainer":
		*a = AccessLevelMaintainer
	case "owner":
		*a = AccessLevelOwner
	default:
		n, err := strconv.Atoi(value.Value)
		if err != nil {
			return fmt.Errorf("unknown access level %q (valid: guest, reporter, developer, maintainer, owner)", value.Value)
		}
		switch AccessLevelValue(n) {
		case AccessLevelGuest, AccessLevelReporter, AccessLevelDeveloper,
			AccessLevelMaintainer, AccessLevelOwner:
			*a = AccessLevelValue(n)
		default:
			return fmt.Errorf("invalid access level %d (valid: 10, 20, 30, 40, 50)", n)
		}
	}
	return nil
}

type APISeedConfig struct {
	Releases      []map[string]interface{} `yaml:"releases"`
	MergeRequests []map[string]interface{} `yaml:"merge_requests"`
	Labels        []map[string]interface{} `yaml:"labels"`
	Pipelines     []map[string]interface{} `yaml:"pipelines"`
	Jobs          []map[string]interface{} `yaml:"jobs"`
}

type MocksConfig struct {
	Binaries map[string]BinaryMockConfig `yaml:"binaries"`
}

type BinaryMockConfig struct {
	Executable string `yaml:"executable"`
}
