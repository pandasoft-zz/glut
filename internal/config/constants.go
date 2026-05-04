package config

const (
	DefaultBranchName = "main"
	DefaultUserName   = "Test User"
	DefaultUserEmail  = "test@example.com"

	PipelineSourcePush     = "push"
	PipelineSourceWeb      = "web"
	PipelineSourceMR       = "merge_request_event"
	PipelineSourceSchedule = "schedule"
	PipelineSourceTrigger  = "trigger"
	PipelineSourceAPI      = "api"
	PipelineSourceParent   = "parent_pipeline"
	PipelineSourceChat     = "chat"
)
