package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/salehkreiner/bridge-claude-code/internal/config"
)

// TestMain lets this test binary impersonate the bridge for the end-to-end test.
// When SCRUB_TEST_ROLE=bridge it runs the real run() logic instead of the test
// suite, so we exercise the genuine resolve -> health-check -> exec handoff path
// (including the platform-specific exec) without compiling a separate binary.
func TestMain(m *testing.M) {
	if os.Getenv("SCRUB_TEST_ROLE") == "bridge" {
		os.Exit(run(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// --- helpers -----------------------------------------------------------------

// startHealthyHub stands up a stub that answers any path with {"status":"ok"}.
// Point SCRUBADUBBER_HUB_CONTROL_URL at its URL to make the health check pass.
func startHealthyHub(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// writeScript writes a tiny executable (a .bat on Windows, an sh script
// elsewhere) and returns its path. It stands in for the real claude binary.
func writeScript(t *testing.T, unixBody, winBody string) string {
	t.Helper()
	dir := t.TempDir()
	var name, content string
	if runtime.GOOS == "windows" {
		name, content = "fakeclaude.bat", "@echo off\r\n"+winBody
	} else {
		name, content = "fakeclaude.sh", "#!/bin/sh\n"+unixBody
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// noopClaude is an executable that exists and exits 0 — enough for resolveClaude.
func noopClaude(t *testing.T) string {
	return writeScript(t, "exit 0\n", "exit /b 0\r\n")
}

// hubEnv blanks the bridge env then sets the common happy-path values.
func hubEnv(t *testing.T, controlURL, claudeBin string) {
	t.Helper()
	for _, k := range []string{config.EnvHubURL, config.EnvHubURLFallback, config.EnvControlURL, config.EnvTimeout, config.EnvClaudeBin, "ANTHROPIC_BASE_URL"} {
		t.Setenv(k, "")
	}
	t.Setenv(config.EnvHubURL, "http://hub.test:8383")
	t.Setenv(config.EnvControlURL, controlURL)
	t.Setenv(config.EnvClaudeBin, claudeBin)
	t.Setenv(config.EnvTimeout, "3000")
}

func envValues(env []string, key string) []string {
	var out []string
	for _, kv := range env {
		if eq := strings.IndexByte(kv, '='); eq >= 0 && strings.EqualFold(kv[:eq], key) {
			out = append(out, kv)
		}
	}
	return out
}

// --- prepare() logic ---------------------------------------------------------

func TestPrepare_Success(t *testing.T) {
	hubEnv(t, startHealthyHub(t), noopClaude(t))

	p, code, err := prepare([]string{"chat", "--model", "x"})
	if err != nil {
		t.Fatalf("prepare returned error (code %d): %v", code, err)
	}
	got := envValues(p.env, "ANTHROPIC_BASE_URL")
	want := "ANTHROPIC_BASE_URL=http://hub.test:8383/anthropic"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ANTHROPIC_BASE_URL = %v, want exactly [%q]", got, want)
	}
	if strings.Join(p.args, " ") != "chat --model x" {
		t.Fatalf("args = %v, want [chat --model x]", p.args)
	}
	if p.claudePath == "" {
		t.Fatal("claudePath is empty")
	}
}

func TestPrepare_OverridesExistingBaseURL(t *testing.T) {
	hubEnv(t, startHealthyHub(t), noopClaude(t))
	t.Setenv("ANTHROPIC_BASE_URL", "http://evil.example/leak")

	p, _, err := prepare(nil)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got := envValues(p.env, "ANTHROPIC_BASE_URL")
	want := "ANTHROPIC_BASE_URL=http://hub.test:8383/anthropic"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("expected the pre-existing value to be replaced; got %v", got)
	}
}

func TestPrepare_HubDown(t *testing.T) {
	// A server that is immediately closed gives us a refusing endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()

	hubEnv(t, dead, noopClaude(t))
	t.Setenv(config.EnvTimeout, "500")

	p, code, err := prepare(nil)
	if err == nil {
		t.Fatal("expected an error when the Hub is down")
	}
	if p != nil {
		t.Fatal("plan must be nil on failure")
	}
	if code != exitHubDown {
		t.Fatalf("code = %d, want %d", code, exitHubDown)
	}
	if !strings.Contains(err.Error(), "control") {
		t.Fatalf("error should mention the control plane / control API; got:\n%s", err)
	}
}

func TestPrepare_BadHubURL(t *testing.T) {
	hubEnv(t, "", noopClaude(t))
	t.Setenv(config.EnvHubURL, "not-a-url")

	_, code, err := prepare(nil)
	if err == nil {
		t.Fatal("expected an error for a malformed Hub URL")
	}
	if code != exitConfig {
		t.Fatalf("code = %d, want %d", code, exitConfig)
	}
}

func TestPrepare_ClaudeNotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "definitely-not-here")
	hubEnv(t, startHealthyHub(t), missing)

	_, code, err := prepare(nil)
	if err == nil {
		t.Fatal("expected an error when claude cannot be found")
	}
	if code != exitNoClaude {
		t.Fatalf("code = %d, want %d", code, exitNoClaude)
	}
}

func TestPrepare_RefusesSelf(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine own executable: %v", err)
	}
	hubEnv(t, startHealthyHub(t), self)

	_, code, err := prepare(nil)
	if err == nil {
		t.Fatal("expected a refusal when claude resolves to ourselves")
	}
	if code != exitNoClaude {
		t.Fatalf("code = %d, want %d", code, exitNoClaude)
	}
	if !strings.Contains(err.Error(), "recurse") {
		t.Fatalf("error should explain the recursion guard; got:\n%s", err)
	}
}

// --- injectBaseURL unit ------------------------------------------------------

func TestInjectBaseURL(t *testing.T) {
	in := []string{"PATH=/bin", "ANTHROPIC_BASE_URL=old", "HOME=/home/x"}
	out := injectBaseURL(in, "http://hub/anthropic")

	vals := envValues(out, "ANTHROPIC_BASE_URL")
	if len(vals) != 1 || vals[0] != "ANTHROPIC_BASE_URL=http://hub/anthropic" {
		t.Fatalf("got %v, want single injected value", vals)
	}
	if len(envValues(out, "PATH")) != 1 || len(envValues(out, "HOME")) != 1 {
		t.Fatalf("unrelated env vars were dropped: %v", out)
	}
}

// --- end-to-end: re-exec ourselves as the bridge through a fake claude --------

func TestWrapperEndToEnd(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine own executable: %v", err)
	}

	// Fake claude prints the injected base URL and exits 7.
	fake := writeScript(t,
		"printf '%s\\n' \"$ANTHROPIC_BASE_URL\"\nexit 7\n",
		"echo %ANTHROPIC_BASE_URL%\r\nexit /b 7\r\n",
	)
	hub := startHealthyHub(t)

	cmd := exec.Command(self, "chat")
	cmd.Env = append(bridgeEnv(),
		"SCRUB_TEST_ROLE=bridge",
		config.EnvHubURL+"=http://hub.test:8383",
		config.EnvControlURL+"="+hub,
		config.EnvClaudeBin+"="+fake,
		config.EnvTimeout+"=3000",
	)
	out, _ := cmd.CombinedOutput()

	if code := cmd.ProcessState.ExitCode(); code != 7 {
		t.Fatalf("exit code = %d, want 7 (claude's code propagated)\noutput:\n%s", code, out)
	}
	if !strings.Contains(string(out), "http://hub.test:8383/anthropic") {
		t.Fatalf("child did not see the injected ANTHROPIC_BASE_URL\noutput:\n%s", out)
	}
}

// bridgeEnv returns the current environment with every bridge-related variable
// (and the test role) removed, so the end-to-end test controls them explicitly.
func bridgeEnv() []string {
	drop := map[string]bool{
		config.EnvHubURL: true, config.EnvHubURLFallback: true, config.EnvControlURL: true,
		config.EnvTimeout: true, config.EnvClaudeBin: true,
		"ANTHROPIC_BASE_URL": true, "SCRUB_TEST_ROLE": true,
	}
	var out []string
	for _, kv := range os.Environ() {
		if eq := strings.IndexByte(kv, '='); eq >= 0 && drop[strings.ToUpper(kv[:eq])] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
