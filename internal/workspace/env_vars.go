package workspace

import (
	"fmt"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

const (
	DetachedHead = "HEAD"
)

func (w *Workspace) EnvVars(setup parser.SetupConfig, port int, sha string, shortSha string, glutName string) map[string]string {
	defaultBranch := w.defaultBranch(setup)
	env := w.baseEnv(port, sha, shortSha, glutName, defaultBranch)
	applyProjectEnv(env, setup)
	applyPipelineEnv(env, setup, defaultBranch)
	return env
}

func (w *Workspace) defaultBranch(setup parser.SetupConfig) string {
	defaultBranch := config.DefaultBranchName
	if setup.API != nil && setup.API.Project != nil && setup.API.Project.DefaultBranch != "" {
		defaultBranch = setup.API.Project.DefaultBranch
	} else {
		detected := getDefaultBranch(w.WorkspaceDir)
		if detected != "" && detected != DetachedHead {
			defaultBranch = detected
		}
	}

	return defaultBranch
}

func (w *Workspace) baseEnv(port int, sha string, shortSha string, glutName string, defaultBranch string) map[string]string {
	workspacePath := w.WorkspaceDir
	if workspacePath == "" {
		workspacePath = w.Dir
	}

	return map[string]string{
		"CI":                   "true",
		"CI_SERVER_URL":        fmt.Sprintf("http://127.0.0.1:%d", port),
		"CI_API_V4_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/v4", port),
		"CI_PROJECT_ID":        "1",
		"CI_PROJECT_PATH":      "test-group/test-project",
		"CI_PROJECT_NAME":      "test-project",
		"CI_PROJECT_NAMESPACE": "test-group",
		"CI_COMMIT_SHA":        sha,
		"CI_COMMIT_SHORT_SHA":  shortSha,
		"CI_DEFAULT_BRANCH":    defaultBranch,
		"CI_PIPELINE_SOURCE":   config.PipelineSourcePush,
		"CI_PIPELINE_ID":       "1",
		"CI_JOB_TOKEN":         "mock-job-token",
		"CI_REGISTRY":          "registry.example.com",
		"CI_REGISTRY_IMAGE":    "registry.example.com/test-group/test-project",
		"GITLAB_USER_NAME":     config.DefaultUserName,
		"GITLAB_USER_EMAIL":    config.DefaultUserEmail,
		"GITLAB_USER_LOGIN":    "test-user",
		"GLUT_WORKSPACE":       workspacePath,
		"GLUT_TEST_NAME":       glutName,
		"GLUT_ORIGIN_REPO":     w.OriginRepo,
		"CI_REPOSITORY_URL":    "file://" + w.OriginRepo,
	}
}

func applyProjectEnv(env map[string]string, setup parser.SetupConfig) {
	if setup.API != nil && setup.API.Project != nil && setup.API.Project.Path != "" {
		path := setup.API.Project.Path
		env["CI_PROJECT_PATH"] = path
		parts := strings.Split(path, "/")
		env["CI_PROJECT_NAME"] = parts[len(parts)-1]
		env["CI_PROJECT_NAMESPACE"] = strings.Join(parts[:len(parts)-1], "/")
		env["CI_REGISTRY_IMAGE"] = "registry.example.com/" + path
	}
}

func applyPipelineEnv(env map[string]string, setup parser.SetupConfig, defaultBranch string) {
	source := config.PipelineSourcePush
	if setup.PipelineSource != "" {
		source = setup.PipelineSource
	}
	env["CI_PIPELINE_SOURCE"] = source

	if source != config.PipelineSourceMR {
		applyBranchOrTagEnv(env, setup, defaultBranch)
	}

	switch source {
	case config.PipelineSourceMR:
		applyMergeRequestEnv(env, setup)
	case config.PipelineSourceSchedule:
		env["CI_PIPELINE_SCHEDULE"] = "true"
		if setup.Schedule != nil {
			env["CI_SCHEDULE_DESCRIPTION"] = setup.Schedule.Description
		}
	case config.PipelineSourceTrigger:
		env["CI_PIPELINE_TRIGGERED"] = "true"
		env["CI_TRIGGER_SHORT_TOKEN"] = "glut"
	case config.PipelineSourceAPI:
		// nothing extra
	case config.PipelineSourceParent:
		if setup.Upstream != nil {
			env["CI_UPSTREAM_PIPELINE_ID"] = fmt.Sprintf("%d", setup.Upstream.PipelineID)
			env["CI_UPSTREAM_PROJECT_ID"] = fmt.Sprintf("%d", setup.Upstream.ProjectID)
			env["CI_UPSTREAM_JOB_ID"] = fmt.Sprintf("%d", setup.Upstream.JobID)
		}
	case config.PipelineSourceChat:
		if setup.Chat != nil {
			env["CI_CHAT_INPUT"] = setup.Chat.Input
			env["CI_CHAT_CHANNEL"] = setup.Chat.Channel
		}
	}
}

func applyBranchOrTagEnv(env map[string]string, setup parser.SetupConfig, defaultBranch string) {
	if setup.Tag != "" {
		env["CI_COMMIT_TAG"] = setup.Tag
		env["CI_COMMIT_REF_NAME"] = setup.Tag
		env["CI_COMMIT_REF_SLUG"] = slugify(setup.Tag)
		return
	}

	branch := defaultBranch
	if setup.Branch != "" {
		branch = setup.Branch
	}
	env["CI_COMMIT_BRANCH"] = branch
	env["CI_COMMIT_REF_NAME"] = branch
	env["CI_COMMIT_REF_SLUG"] = slugify(branch)
	env["CI_COMMIT_REF_PROTECTED"] = "false"
	env["CI_COMMIT_BEFORE_SHA"] = "0000000000000000000000000000000000000000"
}

func applyMergeRequestEnv(env map[string]string, setup parser.SetupConfig) {
	if setup.MergeRequest != nil {
		env["CI_MERGE_REQUEST_IID"] = fmt.Sprintf("%d", setup.MergeRequest.IID)
		env["CI_MERGE_REQUEST_TITLE"] = setup.MergeRequest.Title
		env["CI_MERGE_REQUEST_TARGET_BRANCH_NAME"] = setup.MergeRequest.TargetBranch
		env["CI_MERGE_REQUEST_SOURCE_BRANCH_NAME"] = setup.Branch
		env["CI_MERGE_REQUEST_DRAFT"] = fmt.Sprintf("%t", setup.MergeRequest.Draft)
		env["CI_MERGE_REQUEST_LABELS"] = setup.MergeRequest.Labels
		env["CI_MERGE_REQUEST_ASSIGNEES"] = setup.MergeRequest.Assignees
	}
	env["CI_COMMIT_REF_NAME"] = setup.Branch
	env["CI_COMMIT_REF_SLUG"] = slugify(setup.Branch)
	env["CI_MERGE_REQUEST_PROJECT_ID"] = "1"
	env["CI_MERGE_REQUEST_PROJECT_PATH"] = env["CI_PROJECT_PATH"]
}
