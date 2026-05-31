// Command scrub-claude is the user-facing bridge. It makes Claude Code talk to
// the Scrubadubber Hub instead of talking to Anthropic directly, then hands off
// to the real claude binary.
//
// The whole thing is four steps and nothing more:
//
//  1. Resolve the Hub URL (env, with a loopback default).
//  2. Health-check the Hub's control plane. If it is unreachable, refuse to start
//     so traffic is never sent to Anthropic unprotected.
//  3. Set ANTHROPIC_BASE_URL=<hub>/anthropic in the child environment.
//  4. Exec the real claude, passing every argument straight through and
//     propagating its exit code.
//
// It never reads, stores, or transforms request payloads. It does not touch
// credentials. On success it prints nothing — running scrub-claude should feel
// exactly like running claude.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/salehkreiner/bridge-claude-code/internal/config"
	"github.com/salehkreiner/bridge-claude-code/internal/hubclient"
)

// version is stamped at build time via -ldflags "-X main.version=...". It is not
// printed during normal operation (transparency); it exists for diagnostics.
var version = "dev"

// Exit codes. claude's own exit code is propagated unchanged on success; these
// are only used when the bridge itself refuses to proceed, so scripts can tell
// "the bridge stopped me" apart from "claude exited non-zero".
const (
	exitConfig   = 78  // EX_CONFIG     — bad configuration (e.g. malformed Hub URL)
	exitHubDown  = 69  // EX_UNAVAILABLE — Hub control plane unreachable
	exitLaunch   = 126 // found claude but could not execute it
	exitNoClaude = 127 // could not find claude at all
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run performs the bridge's work and returns the process exit code. On a
// successful Unix handoff it does not return (the process image is replaced).
func run(args []string) int {
	p, code, err := prepare(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return code
	}

	exitCode, herr := handoff(p.claudePath, p.args, p.env)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "scrub-claude: could not launch %s: %v\n", p.claudePath, herr)
		return exitLaunch
	}
	return exitCode
}

// plan is everything needed to hand off to claude.
type plan struct {
	claudePath string
	args       []string
	env        []string
}

// prepare runs the resolve + health-check + locate + env-inject steps. It returns
// either a ready plan, or an exit code and an error explaining why the bridge will
// not proceed. It performs no exec, which makes it straightforward to test.
func prepare(args []string) (*plan, int, error) {
	hubURL, err := config.ResolveHubURL()
	if err != nil {
		return nil, exitConfig, fmt.Errorf("scrub-claude: %w", err)
	}

	healthURL, err := config.ControlHealthURL(hubURL)
	if err != nil {
		return nil, exitConfig, fmt.Errorf("scrub-claude: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout())
	defer cancel()
	if _, err := hubclient.Check(ctx, healthURL); err != nil {
		return nil, exitHubDown, hubDownError(hubURL, healthURL, err)
	}

	claudePath, err := resolveClaude()
	if err != nil {
		return nil, exitNoClaude, fmt.Errorf("scrub-claude: %w", err)
	}

	env := injectBaseURL(os.Environ(), config.AnthropicBaseURL(hubURL))
	return &plan{claudePath: claudePath, args: args, env: env}, 0, nil
}

// resolveClaude finds the real claude binary: CLAUDE_BIN if set, otherwise the
// first claude on PATH. It refuses to resolve to scrub-claude itself.
func resolveClaude() (string, error) {
	override := config.ClaudeBin()
	target := override
	if target == "" {
		target = "claude"
	}

	path, err := exec.LookPath(target)
	if err != nil {
		if override != "" {
			return "", fmt.Errorf("%s=%q is not a runnable executable: %w", config.EnvClaudeBin, override, err)
		}
		return "", fmt.Errorf("could not find the 'claude' binary on PATH; install Claude Code or set %s to its full path", config.EnvClaudeBin)
	}

	if isSelf(path) {
		return "", fmt.Errorf("claude resolved to scrub-claude itself (%s); refusing to recurse — check %s and any 'claude' alias", path, config.EnvClaudeBin)
	}
	return path, nil
}

// isSelf reports whether path is the same on-disk file as this running binary.
func isSelf(path string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	selfInfo, err1 := os.Stat(self)
	pathInfo, err2 := os.Stat(path)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(selfInfo, pathInfo)
}

// injectBaseURL returns a copy of environ with ANTHROPIC_BASE_URL forced to
// baseURL, dropping any pre-existing value (case-insensitively, for Windows).
func injectBaseURL(environ []string, baseURL string) []string {
	const key = "ANTHROPIC_BASE_URL"
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if eq := strings.IndexByte(kv, '='); eq >= 0 && strings.EqualFold(kv[:eq], key) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, key+"="+baseURL)
}

// hubDownError builds the actionable, fail-closed message shown when the Hub
// control plane cannot be reached.
func hubDownError(hubURL, healthURL string, cause error) error {
	return fmt.Errorf(`scrub-claude: cannot reach the Scrubadubber Hub control plane.

  Hub URL:        %s
  Health checked: %s
  Error:          %v

Refusing to start, so your traffic is NOT sent to Anthropic unprotected.

Likely causes:
  - the Hub is not running                -> start the Hub
  - %s points at the wrong host or port   -> check it
  - the Hub's control API is disabled     -> enable review.enabled and
                                             review.control_api.enabled in the Hub config`,
		hubURL, healthURL, cause, config.EnvHubURL)
}
