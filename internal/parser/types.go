package parser

import "github.com/pandasoft-zz/glut/internal/config"

type TestFile struct {
	FilePath     string
	Glut         GlutSection
	PipelineYAML string
}

type GlutSection struct {
	Name   string             `yaml:"name"`
	Setup  config.SetupConfig `yaml:"setup"`
	Assert AssertConfig       `yaml:"assert"`
}

type SetupConfig = config.SetupConfig
type MRConfig = config.MRConfig
type UpstreamConfig = config.UpstreamConfig
type ScheduleConfig = config.ScheduleConfig
type ChatConfig = config.ChatConfig
type GitSetupConfig = config.GitSetupConfig
type GitUserConfig = config.GitUserConfig
type GitOriginConfig = config.GitOriginConfig
type APISetupConfig = config.APISetupConfig
type TokenConfig = config.TokenConfig
type ProjectConfig = config.ProjectConfig
type APISeedConfig = config.APISeedConfig
type MocksConfig = config.MocksConfig
type BinaryMockConfig = config.BinaryMockConfig

type AssertConfig struct {
	Job       map[string]JobAssert      `yaml:"job"`
	Artifacts map[string]ArtifactAssert `yaml:"artifacts"`
	Git       *GitAssert                `yaml:"git"`
	API       map[string]APICallAssert  `yaml:"api"`
	Binary    map[string]BinaryAssert   `yaml:"binary"`
}

// Stubs for TICKET-06
type JobAssert struct{}
type ArtifactAssert struct{}
type GitAssert struct{}
type APICallAssert struct{}
type BinaryAssert struct{}

// Linting
type LintLevel int

const (
	LevelWarning LintLevel = iota
	LevelError
)

type LintError struct {
	File    string
	Line    int
	Level   LintLevel
	Message string
}
