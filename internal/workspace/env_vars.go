package workspace

import (
	"fmt"
	"os"
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
	applyPipelineUserEnv(env, setup)
	if setup.Mocks != nil && len(setup.Mocks.Binaries) > 0 {
		effectiveHostEnv := w.hostEnv
		if effectiveHostEnv == nil {
			effectiveHostEnv = os.Environ()
		}
		env["PATH"] = envSliceToMap(effectiveHostEnv)["PATH"]
		AddMockBinaryEnv(env, w.Dir)
	}
	return env
}

func (w *Workspace) defaultBranch(setup parser.SetupConfig) string {
	// w.DefaultBranch is set by New() via resolveDefaultBranch(), which already
	// covers setup.default_branch, api.project.default_branch (deprecated), and
	// origin/HEAD detection from the real source repo.
	if w.DefaultBranch != "" {
		return w.DefaultBranch
	}
	// Fallback for Workspace instances not created via New() (e.g. direct test
	// construction). Still honour the deprecated api.project.default_branch.
	if setup.API != nil && setup.API.Project != nil && setup.API.Project.DefaultBranch != "" {
		return setup.API.Project.DefaultBranch
	}
	return config.DefaultBranchName
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
		"GITLAB_USER_LOGIN":    config.DefaultUserLogin,
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

// applyPipelineUserEnv sets GITLAB_USER_NAME, GITLAB_USER_EMAIL, and
// GITLAB_USER_LOGIN using the following priority chain:
//
//  1. setup.pipeline.user  — explicit pipeline user config
//  2. setup.git.user       — git committer identity (same person, shared by default)
//  3. config defaults      — "Test User" / "test@example.com" / "test-user"
func applyPipelineUserEnv(env map[string]string, setup parser.SetupConfig) {
	name := config.DefaultUserName
	email := config.DefaultUserEmail
	login := config.DefaultUserLogin

	if setup.Git != nil {
		if setup.Git.User.Name != "" {
			name = setup.Git.User.Name
		}
		if setup.Git.User.Email != "" {
			email = setup.Git.User.Email
		}
	}

	if setup.Pipeline != nil && setup.Pipeline.User != nil {
		if setup.Pipeline.User.Name != "" {
			name = setup.Pipeline.User.Name
		}
		if setup.Pipeline.User.Email != "" {
			email = setup.Pipeline.User.Email
		}
		if setup.Pipeline.User.Login != "" {
			login = setup.Pipeline.User.Login
		}
	}

	env["GITLAB_USER_NAME"] = name
	env["GITLAB_USER_EMAIL"] = email
	env["GITLAB_USER_LOGIN"] = login
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
