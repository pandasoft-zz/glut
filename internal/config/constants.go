package config

const (
	DefaultBranchName = "main"
	DefaultUserName   = "Test User"
	DefaultUserEmail  = "test@example.com"
	DefaultUserLogin  = "test-user"
	MockJobToken      = "mock-job-token"

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

	// TestedGCLVersion is the gitlab-ci-local version GLUT is developed and
	// CI-tested against. Job results are recovered from gitlab-ci-local's
	// human-oriented output (it has no machine-readable run result as of this
	// version), so a different version may change the wording and break
	// parsing. Keep in sync with GCL_VERSION in the Dockerfile.
	TestedGCLVersion = "4.72.0"
)
