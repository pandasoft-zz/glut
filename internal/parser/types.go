package parser

import "github.com/pandasoft-zz/glut/internal/config"

type TestFile struct {
	FilePath     string
	Glut         GlutSection
	GlutRaw      map[string]interface{}
	PipelineYAML string
	ParseError   error
}

type GlutSection struct {
	Name   string              `yaml:"name"`
	Setup  SetupConfig         `yaml:"setup"`
	Assert config.AssertConfig `yaml:"assert"`
}

type SetupConfig = config.SetupConfig
type PipelineConfig = config.PipelineConfig
type PipelineUserConfig = config.PipelineUserConfig
type MRConfig = config.MRConfig
type UpstreamConfig = config.UpstreamConfig
type ScheduleConfig = config.ScheduleConfig
type ChatConfig = config.ChatConfig
type GitSetupConfig = config.GitSetupConfig
type GitUserConfig = config.GitUserConfig
type GitOriginConfig = config.GitOriginConfig
type APISetupConfig = config.APISetupConfig
type UserConfig = config.UserConfig
type GroupConfig = config.GroupConfig
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
