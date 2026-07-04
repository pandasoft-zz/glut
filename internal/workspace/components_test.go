package workspace

import (
	"strings"
	"testing"
)

func TestRealComponentFetch_FromCIEnv(t *testing.T) {
	env := []string{
		"CI_SERVER_HOST=gitlab.example.com",
		"CI_JOB_TOKEN=secret-token",
		"CI_PROJECT_NAMESPACE=acme/systems/ci",
		"CI_PROJECT_PATH=acme/systems/ci/composer",
	}

	got, err := RealComponentFetch(env)
	if err != nil {
		t.Fatalf("RealComponentFetch returned error: %v", err)
	}

	if got.Namespace != "acme/systems/ci" {
		t.Errorf("Namespace = %q, want %q", got.Namespace, "acme/systems/ci")
	}
	if got.ServerFQDN != "gitlab.example.com:443" {
		t.Errorf("ServerFQDN = %q, want %q", got.ServerFQDN, "gitlab.example.com:443")
	}
	// Port defaults to 443 for https; URL is credential-free.
	wantURL := "https://gitlab.example.com:443/acme/systems/ci/composer.git"
	if got.GCLOriginURL != wantURL {
		t.Errorf("GCLOriginURL = %q, want %q", got.GCLOriginURL, wantURL)
	}
	if strings.Contains(got.GCLOriginURL, "secret-token") {
		t.Errorf("gcl-origin URL must not carry the token: %q", got.GCLOriginURL)
	}

	if got.GitConfigEnv["GIT_CONFIG_COUNT"] != "1" {
		t.Errorf("GIT_CONFIG_COUNT = %q, want 1", got.GitConfigEnv["GIT_CONFIG_COUNT"])
	}
	wantKey := "url.https://gitlab-ci-token:secret-token@gitlab.example.com:443/.insteadOf"
	if got.GitConfigEnv["GIT_CONFIG_KEY_0"] != wantKey {
		t.Errorf("GIT_CONFIG_KEY_0 = %q, want %q", got.GitConfigEnv["GIT_CONFIG_KEY_0"], wantKey)
	}
	// The insteadOf "original" prefix must carry the explicit port so it matches
	// the URL gitlab-ci-local builds (https://host:443/<path>.git).
	wantVal := "https://gitlab.example.com:443/"
	if got.GitConfigEnv["GIT_CONFIG_VALUE_0"] != wantVal {
		t.Errorf("GIT_CONFIG_VALUE_0 = %q, want %q", got.GitConfigEnv["GIT_CONFIG_VALUE_0"], wantVal)
	}
}

func TestRealComponentFetch_GLUTOverridesAndServerWithPort(t *testing.T) {
	env := []string{
		// Real CI values present, but GLUT_* overrides win. CI_SERVER_PORT is
		// always set inside a real CI job — it must not beat the port
		// embedded in GLUT_COMPONENTS_SERVER.
		"CI_SERVER_HOST=gitlab.example.com",
		"CI_SERVER_PORT=443",
		"CI_JOB_TOKEN=ci-token",
		"CI_PROJECT_NAMESPACE=acme/ci",
		"CI_PROJECT_PATH=acme/ci/proj",
		"GLUT_COMPONENTS_SERVER=git.internal:8443",
		"GLUT_COMPONENTS_TOKEN=pat-token",
		"GLUT_COMPONENTS_NAMESPACE=team/group",
	}

	got, err := RealComponentFetch(env)
	if err != nil {
		t.Fatalf("RealComponentFetch returned error: %v", err)
	}

	if got.Namespace != "team/group" {
		t.Errorf("Namespace = %q, want %q", got.Namespace, "team/group")
	}
	// Port parsed from GLUT_COMPONENTS_SERVER "host:port".
	wantVal := "https://git.internal:8443/"
	if got.GitConfigEnv["GIT_CONFIG_VALUE_0"] != wantVal {
		t.Errorf("GIT_CONFIG_VALUE_0 = %q, want %q", got.GitConfigEnv["GIT_CONFIG_VALUE_0"], wantVal)
	}
	if !strings.Contains(got.GitConfigEnv["GIT_CONFIG_KEY_0"], "gitlab-ci-token:pat-token@git.internal:8443/") {
		t.Errorf("GIT_CONFIG_KEY_0 missing override creds/host: %q", got.GitConfigEnv["GIT_CONFIG_KEY_0"])
	}
}

// TestRealComponentFetch_GLUTComponentsPortRanksHighest verifies the full
// port precedence: GLUT_COMPONENTS_PORT > port embedded in
// GLUT_COMPONENTS_SERVER > CI_SERVER_PORT > 443.
func TestRealComponentFetch_GLUTComponentsPortRanksHighest(t *testing.T) {
	env := []string{
		"CI_SERVER_HOST=gitlab.example.com",
		"CI_SERVER_PORT=443",
		"CI_JOB_TOKEN=t",
		"CI_PROJECT_NAMESPACE=acme",
		"GLUT_COMPONENTS_SERVER=git.internal:8443",
		"GLUT_COMPONENTS_PORT=9999",
	}
	got, err := RealComponentFetch(env)
	if err != nil {
		t.Fatalf("RealComponentFetch returned error: %v", err)
	}
	if got.ServerFQDN != "git.internal:9999" {
		t.Errorf("ServerFQDN = %q, want %q", got.ServerFQDN, "git.internal:9999")
	}
}

func TestRealComponentFetch_SyntheticProjectWhenPathHasNoSlash(t *testing.T) {
	env := []string{
		"CI_SERVER_HOST=gitlab.example.com",
		"CI_JOB_TOKEN=t",
		"CI_PROJECT_NAMESPACE=topgroup",
		// CI_PROJECT_PATH absent → gcl-origin needs >=2 path segments.
	}
	got, err := RealComponentFetch(env)
	if err != nil {
		t.Fatalf("RealComponentFetch returned error: %v", err)
	}
	if !strings.HasPrefix(got.GCLOriginURL, "https://gitlab.example.com:443/topgroup/") ||
		!strings.HasSuffix(got.GCLOriginURL, ".git") {
		t.Errorf("synthetic gcl-origin URL malformed: %q", got.GCLOriginURL)
	}
}

func TestRealComponentFetch_MissingCoordinates(t *testing.T) {
	_, err := RealComponentFetch([]string{"CI_SERVER_HOST=gitlab.example.com"})
	if err == nil {
		t.Fatal("expected error when token and namespace are missing")
	}
	if !strings.Contains(err.Error(), "CI_JOB_TOKEN") || !strings.Contains(err.Error(), "CI_PROJECT_NAMESPACE") {
		t.Errorf("error should name the missing coordinates, got: %v", err)
	}
}
