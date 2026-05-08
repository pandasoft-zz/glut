package parser

import "github.com/pandasoft-zz/glut/internal/config"

type TestFile struct {
	FilePath     string
	Glut         GlutSection
	PipelineYAML string
}

type GlutSection struct {
	Name   string              `yaml:"name"`
	Setup  SetupConfig         `yaml:"setup"`
	Assert config.AssertConfig `yaml:"assert"`
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
