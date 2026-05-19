package config

const (
	DefaultBranchName = "main"
	DefaultUserName   = "Test User"
	DefaultUserEmail  = "test@example.com"
	DefaultUserLogin  = "test-user"

	PipelineSourcePush     = "push"
	PipelineSourceWeb      = "web"
	PipelineSourceMR       = "merge_request_event"
	PipelineSourceSchedule = "schedule"
	PipelineSourceTrigger  = "trigger"
	PipelineSourceAPI      = "api"
	PipelineSourceParent   = "parent_pipeline"
	PipelineSourceChat     = "chat"

	EnvMockLogDir  = "GLUT_MOCK_LOG_DIR"
	EnvMockBinReal = "GLUT_MOCK_BIN_REAL"
)
