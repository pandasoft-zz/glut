package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/parser"
	"github.com/pandasoft-zz/glut/internal/workspace"
)

// TestBuildExecConfigDockerMode drives the buildExecConfig phase directly
// against a real workspace and a running mock server, pinning the Docker-mode
// wiring: bridge-reachable server URL, extra hosts, and the GCL compatibility
// env var.
func TestBuildExecConfigDockerMode(t *testing.T) {
	env := newRunnerTestEnv(t)

	// The workspace snapshot commit needs at least one file in the source repo,
	// otherwise the origin has no HEAD for gitHeads to read.
	env.writeRawFile(t, "README.md", "content\n")

	work, err := workspace.New(parser.SetupConfig{}, false, env.repoDir, workspace.Options{HostEnv: env.hostEnv})
	if err != nil {
		t.Fatalf("workspace.New() error = %v", err)
	}
	t.Cleanup(func() { _ = work.Destroy() })

	server, err := mockserver.New(parser.APISetupConfig{})
	if err != nil {
		t.Fatalf("mockserver.New() error = %v", err)
	}
	if err := server.Start(false); err != nil {
		t.Fatalf("server.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	r := &testRun{
		suite: &suiteRun{opts: RunOptions{Timeout: time.Minute, HostEnv: env.hostEnv}},
		testFile: parser.TestFile{
			PipelineYAML: "job:\n  script: echo ok\n",
			Glut:         parser.GlutSection{Name: "exec config test"},
		},
		useDocker:    true,
		work:         work,
		server:       server,
		phaseTimings: map[string]time.Duration{},
	}
	if err := r.buildExecConfig(); err != nil {
		t.Fatalf("buildExecConfig() error = %v", err)
	}

	cfg := r.execCfg
	if cfg.WorkspacePath != work.WorkspaceDir {
		t.Fatalf("WorkspacePath = %q, want %q", cfg.WorkspacePath, work.WorkspaceDir)
	}
	if cfg.Timeout != time.Minute || !cfg.UseDocker || cfg.ForceShell {
		t.Fatalf("cfg basics = %+v", cfg)
	}
	if sha := cfg.EnvVars["CI_COMMIT_SHA"]; len(sha) != 40 {
		t.Fatalf("CI_COMMIT_SHA = %q, want a full SHA", sha)
	}
	if cfg.EnvVars["GCL_UMASK"] != "false" {
		t.Fatal("Docker mode must set GCL_UMASK=false for rootless images")
	}
	if r.mockHostIP == "" || !strings.Contains(cfg.EnvVars["CI_SERVER_URL"], r.mockHostIP) {
		t.Fatalf("CI_SERVER_URL = %q, want the bridge-reachable IP %q", cfg.EnvVars["CI_SERVER_URL"], r.mockHostIP)
	}
	if len(cfg.DockerExtraHosts) != 2 {
		t.Fatalf("DockerExtraHosts = %v, want host.docker.internal and glut-mock", cfg.DockerExtraHosts)
	}
	if _, ok := r.phaseTimings["git-head"]; !ok {
		t.Fatal("git-head phase timing not recorded")
	}
}
