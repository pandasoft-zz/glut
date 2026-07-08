package workspace

import (
	"fmt"
	"net/url"
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
	applyDerivedURLEnv(env)
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

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	return map[string]string{
		"CI":            "true",
		"CI_SERVER_URL": serverURL,
		// CI_SERVER_PROTOCOL/HOST/PORT/FQDN must be set explicitly: gitlab-ci-local
		// derives them from the workspace git remote, and because GLUT's origin is a
		// local path it cannot parse, GCL falls back to https/gitlab.com/443. Those
		// fallback values leak into jobs and break tools that compose URLs from them
		// (issue #83).
		"CI_SERVER_PROTOCOL":   "http",
		"CI_SERVER_HOST":       "127.0.0.1",
		"CI_SERVER_PORT":       fmt.Sprintf("%d", port),
		"CI_SERVER_FQDN":       fmt.Sprintf("127.0.0.1:%d", port),
		"CI_SERVER_NAME":       "GitLab",
		"CI_SERVER_VERSION":    "16.11.0",
		"CI_SERVER_REVISION":   "mock",
		"CI_API_V4_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/v4", port),
		"CI_PROJECT_ID":        "1",
		"CI_PROJECT_PATH":      "test-group/test-project",
		"CI_PROJECT_NAME":      "test-project",
		"CI_PROJECT_NAMESPACE": "test-group",
		// CI_PROJECT_PATH_SLUG/ROOT_NAMESPACE/TITLE, CI_PIPELINE_IID, and GITLAB_CI
		// must be explicit for the same reason as the CI_SERVER_* family above:
		// gitlab-ci-local derives them from the git remote and falls back to
		// "fallback.group"/"fallback.project" (and GITLAB_CI=false) when the remote
		// is a local path it cannot parse.
		"CI_PROJECT_PATH_SLUG":      slugify("test-group/test-project"),
		"CI_PROJECT_ROOT_NAMESPACE": "test-group",
		"CI_PROJECT_TITLE":          "test-project",
		"CI_COMMIT_SHA":             sha,
		"CI_COMMIT_SHORT_SHA":       shortSha,
		"CI_DEFAULT_BRANCH":         defaultBranch,
		"CI_PIPELINE_SOURCE":        config.PipelineSourcePush,
		"CI_PIPELINE_ID":            "1",
		"CI_PIPELINE_IID":           "1",
		"CI_JOB_ID":                 "1",
		"CI_JOB_TOKEN":              config.MockJobToken,
		"GITLAB_CI":                 "true",
		"CI_REGISTRY":               "registry.example.com",
		"CI_REGISTRY_IMAGE":         "registry.example.com/test-group/test-project",
		"GITLAB_USER_NAME":          config.DefaultUserName,
		"GITLAB_USER_EMAIL":         config.DefaultUserEmail,
		"GITLAB_USER_LOGIN":         config.DefaultUserLogin,
		"GITLAB_USER_ID":            "1",
		"GLUT_WORKSPACE":            workspacePath,
		"GLUT_TEST_NAME":            glutName,
		"GLUT_ORIGIN_REPO":          w.OriginRepo,
	}
}

// ApplyServerBaseURL points every CI variable derived from the mock server URL
// at http://host:port. The runner uses it in Docker mode, where containers
// cannot reach 127.0.0.1 and the bridge IP must be used instead (issue #83:
// the whole CI_SERVER_* family must stay consistent, not just CI_SERVER_URL).
func ApplyServerBaseURL(env map[string]string, host string, port int) {
	env["CI_SERVER_URL"] = fmt.Sprintf("http://%s:%d", host, port)
	env["CI_API_V4_URL"] = fmt.Sprintf("http://%s:%d/api/v4", host, port)
	env["CI_SERVER_HOST"] = host
	env["CI_SERVER_PORT"] = fmt.Sprintf("%d", port)
	env["CI_SERVER_FQDN"] = fmt.Sprintf("%s:%d", host, port)
	applyDerivedURLEnv(env)
	if _, ok := env["CI_MERGE_REQUEST_SOURCE_PROJECT_URL"]; ok {
		env["CI_MERGE_REQUEST_SOURCE_PROJECT_URL"] = env["CI_SERVER_URL"] + "/" + env["CI_PROJECT_PATH"]
	}
}

// applyDerivedURLEnv computes URL variables that depend on earlier apply steps
// (project path may have been overridden by applyProjectEnv).
func applyDerivedURLEnv(env map[string]string) {
	serverURL := env["CI_SERVER_URL"]
	projectPath := env["CI_PROJECT_PATH"]
	pipelineID := env["CI_PIPELINE_ID"]
	jobID := env["CI_JOB_ID"]

	projectURL := serverURL + "/" + projectPath
	env["CI_PROJECT_URL"] = projectURL
	env["CI_PIPELINE_URL"] = projectURL + "/-/pipelines/" + pipelineID
	env["CI_JOB_URL"] = projectURL + "/-/jobs/" + jobID

	// CI_REPOSITORY_URL: HTTP URL with embedded job token, matching real GitLab CI
	// format (https://gitlab-ci-token:TOKEN@host/group/project.git). Using HTTP
	// keeps it consistent with CI_SERVER_URL and prevents tools like semantic-release
	// from applying URL transformations that break when the scheme is "file" (issue #83).
	// The mock server serves the origin repo via git smart HTTP at this path.
	if repoURL, err := url.Parse(serverURL); err == nil {
		repoURL.User = url.UserPassword("gitlab-ci-token", env["CI_JOB_TOKEN"])
		repoURL.Path = "/" + projectPath + ".git"
		env["CI_REPOSITORY_URL"] = repoURL.String()
	}

	// Dependency proxy: gitlab-ci-local builds the image prefixes from the git
	// remote group, which degrades to "fallback.group" in GLUT workspaces. Derive
	// them from CI_SERVER_FQDN and the root namespace instead.
	proxyServer := env["CI_SERVER_FQDN"]
	proxyPrefix := proxyServer + "/" + env["CI_PROJECT_ROOT_NAMESPACE"] + "/dependency_proxy/containers"
	env["CI_DEPENDENCY_PROXY_SERVER"] = proxyServer
	env["CI_DEPENDENCY_PROXY_GROUP_IMAGE_PREFIX"] = proxyPrefix
	env["CI_DEPENDENCY_PROXY_DIRECT_GROUP_IMAGE_PREFIX"] = proxyPrefix
	env["CI_DEPENDENCY_PROXY_USER"] = "gitlab-ci-token"
	env["CI_DEPENDENCY_PROXY_PASSWORD"] = env["CI_JOB_TOKEN"]
}

// ApplyCommitEnv sets the commit-message variables from the workspace HEAD
// commit. gitlab-ci-local never reads the real commit message — it hardcodes
// "Commit Title"/"More commit text" placeholders — so these must be passed
// explicitly or rules matching $CI_COMMIT_MESSAGE see placeholder text.
func ApplyCommitEnv(env map[string]string, message string, timestamp string) {
	env["CI_COMMIT_MESSAGE"] = message
	title, description, _ := strings.Cut(message, "\n")
	env["CI_COMMIT_TITLE"] = title
	env["CI_COMMIT_DESCRIPTION"] = strings.TrimLeft(description, "\n")
	if timestamp != "" {
		env["CI_COMMIT_TIMESTAMP"] = timestamp
	}
}

func applyProjectEnv(env map[string]string, setup parser.SetupConfig) {
	if setup.API == nil || setup.API.Project == nil {
		return
	}
	project := setup.API.Project
	if project.Path != "" {
		path := project.Path
		env["CI_PROJECT_PATH"] = path
		parts := strings.Split(path, "/")
		env["CI_PROJECT_NAME"] = parts[len(parts)-1]
		env["CI_PROJECT_NAMESPACE"] = strings.Join(parts[:len(parts)-1], "/")
		env["CI_PROJECT_PATH_SLUG"] = slugify(path)
		env["CI_PROJECT_ROOT_NAMESPACE"] = parts[0]
		env["CI_PROJECT_TITLE"] = parts[len(parts)-1]
		env["CI_REGISTRY_IMAGE"] = "registry.example.com/" + path
	}
	if project.Title != "" {
		env["CI_PROJECT_TITLE"] = project.Title
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
	case config.PipelineSourceChat:
		if setup.Chat != nil {
			env["CI_CHAT_INPUT"] = setup.Chat.Input
			env["CI_CHAT_CHANNEL"] = setup.Chat.Channel
			env["CI_CHAT_USER_ID"] = setup.Chat.UserID
		}
	case config.PipelineSourceAPI:
		// nothing extra
	case config.PipelineSourceParent:
		if setup.Upstream != nil {
			env["CI_UPSTREAM_PIPELINE_ID"] = fmt.Sprintf("%d", setup.Upstream.PipelineID)
			env["CI_UPSTREAM_PROJECT_ID"] = fmt.Sprintf("%d", setup.Upstream.ProjectID)
			env["CI_UPSTREAM_JOB_ID"] = fmt.Sprintf("%d", setup.Upstream.JobID)
		}
	}
}

func applyBranchOrTagEnv(env map[string]string, setup parser.SetupConfig, defaultBranch string) {
	refProtected := "false"
	if setup.RefProtected != nil && *setup.RefProtected {
		refProtected = "true"
	}
	env["CI_COMMIT_REF_PROTECTED"] = refProtected

	env["CI_COMMIT_BEFORE_SHA"] = "0000000000000000000000000000000000000000"

	if setup.Tag != "" {
		env["CI_COMMIT_TAG"] = setup.Tag
		env["CI_COMMIT_REF_NAME"] = setup.Tag
		env["CI_COMMIT_REF_SLUG"] = slugify(setup.Tag)
		env["CI_COMMIT_TAG_MESSAGE"] = setup.TagMessage
		return
	}

	branch := defaultBranch
	if setup.Branch != "" {
		branch = setup.Branch
	}
	env["CI_COMMIT_BRANCH"] = branch
	env["CI_COMMIT_REF_NAME"] = branch
	env["CI_COMMIT_REF_SLUG"] = slugify(branch)
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

// UnsetVars returns variables that gitlab-ci-local would auto-inject from git
// context but that must be absent in the given pipeline type to match real
// GitLab behaviour.
//
// CI_COMMIT_BRANCH: gitlab-ci-local always sets this to the current git branch
// (D.commit.REF_NAME in GCL source) regardless of pipeline source. On real
// GitLab the variable is not available in MR or tag pipelines. Without an
// explicit --unset-variable the workspace branch ("main") leaks into MR and tag
// jobs, causing rules like `if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH` to
// fire incorrectly. Fix tracked in issue #69.
func (w *Workspace) UnsetVars(setup parser.SetupConfig) []string {
	source := config.PipelineSourcePush
	if setup.PipelineSource != "" {
		source = setup.PipelineSource
	}
	if source == config.PipelineSourceMR || setup.Tag != "" {
		return []string{"CI_COMMIT_BRANCH"}
	}
	return nil
}

func applyMergeRequestEnv(env map[string]string, setup parser.SetupConfig) {
	refProtected := "false"
	if setup.RefProtected != nil && *setup.RefProtected {
		refProtected = "true"
	}
	env["CI_COMMIT_REF_PROTECTED"] = refProtected
	env["CI_COMMIT_BEFORE_SHA"] = "0000000000000000000000000000000000000000"

	if setup.MergeRequest != nil {
		env["CI_MERGE_REQUEST_IID"] = fmt.Sprintf("%d", setup.MergeRequest.IID)
		env["CI_MERGE_REQUEST_TITLE"] = setup.MergeRequest.Title
		env["CI_MERGE_REQUEST_TARGET_BRANCH_NAME"] = setup.MergeRequest.TargetBranch
		env["CI_MERGE_REQUEST_SOURCE_BRANCH_NAME"] = setup.Branch
		env["CI_MERGE_REQUEST_DRAFT"] = fmt.Sprintf("%t", setup.MergeRequest.Draft)
		env["CI_MERGE_REQUEST_LABELS"] = setup.MergeRequest.Labels
		env["CI_MERGE_REQUEST_ASSIGNEES"] = setup.MergeRequest.Assignees
		env["CI_MERGE_REQUEST_DESCRIPTION"] = setup.MergeRequest.Description
		env["CI_MERGE_REQUEST_MILESTONE"] = setup.MergeRequest.Milestone
		env["CI_MERGE_REQUEST_SQUASH"] = fmt.Sprintf("%t", setup.MergeRequest.Squash)
	}
	env["CI_COMMIT_REF_NAME"] = setup.Branch
	env["CI_COMMIT_REF_SLUG"] = slugify(setup.Branch)
	env["CI_MERGE_REQUEST_PROJECT_ID"] = "1"
	env["CI_MERGE_REQUEST_PROJECT_PATH"] = env["CI_PROJECT_PATH"]
	env["CI_MERGE_REQUEST_SOURCE_PROJECT_ID"] = "1"
	env["CI_MERGE_REQUEST_SOURCE_PROJECT_PATH"] = env["CI_PROJECT_PATH"]
	env["CI_MERGE_REQUEST_SOURCE_PROJECT_URL"] = env["CI_SERVER_URL"] + "/" + env["CI_PROJECT_PATH"]

	approved := "false"
	eventType := "detached"
	diffBaseSHA := "0000000000000000000000000000000000000000"
	if setup.MergeRequest != nil {
		if setup.MergeRequest.Approved {
			approved = "true"
		}
		if setup.MergeRequest.EventType != "" {
			eventType = setup.MergeRequest.EventType
		}
		if setup.MergeRequest.DiffBaseSHA != "" {
			diffBaseSHA = setup.MergeRequest.DiffBaseSHA
		}
	}
	env["CI_MERGE_REQUEST_APPROVED"] = approved
	env["CI_MERGE_REQUEST_EVENT_TYPE"] = eventType
	env["CI_MERGE_REQUEST_DIFF_BASE_SHA"] = diffBaseSHA
}
