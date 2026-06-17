// Package config is the single source of truth for the CipherBond Hub contract:
// the environment variable names, their defaults, and the small amount of URL
// derivation both binaries need (the Anthropic proxy base and the control-plane
// health endpoint).
//
// Keeping all of this in one place is deliberate: if the Hub contract ever
// changes (a port, the /healthz path, a field name), it is a one-line edit here,
// and an auditor has exactly one file to read to understand how the bridge talks
// to the Hub.
//
// This package only reads the environment and parses URLs. It performs no I/O.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment variables, in the priority order documented in the README.
const (
	// EnvHubURL is the canonical Hub base URL variable.
	EnvHubURL = "CIPHERBOND_HUB_URL"
	// EnvHubURLFallback is accepted when EnvHubURL is unset, for enterprise
	// setups that standardise on a bare HUB_URL.
	EnvHubURLFallback = "HUB_URL"
	// EnvControlURL fully overrides the derived control-plane health URL, for
	// topologies where the control plane is not on <hub-host>:8384.
	EnvControlURL = "CipherBond_HUB_CONTROL_URL"
	// EnvTimeout is the health-check timeout in milliseconds.
	EnvTimeout = "CipherBond_TIMEOUT"
	// EnvClaudeBin overrides the path to the real claude binary.
	EnvClaudeBin = "CLAUDE_BIN"
)

// Hub contract constants. See CLAUDE.md and the live CipherBond-hub repo.
const (
	// DefaultHubURL is the loopback Hub a lone developer runs locally.
	DefaultHubURL = "http://127.0.0.1:8383"
	// DefaultProxyPort is the Hub's Anthropic proxy port.
	DefaultProxyPort = "8383"
	// ControlPort is the Hub's control-plane port, which serves the health check.
	ControlPort = "8384"
	// AnthropicPrefix is appended to the Hub base URL to form ANTHROPIC_BASE_URL.
	AnthropicPrefix = "/anthropic"
	// HealthPath is the control-plane health endpoint. Note: /healthz, not /health,
	// and it only exists when the Hub's review + control_api are both enabled.
	HealthPath = "/healthz"
	// DefaultTimeout bounds the health check so the bridge never stalls a developer.
	DefaultTimeout = 2000 * time.Millisecond
)

// ResolveHubURL returns the normalised Hub base URL (scheme://host[:port], no
// trailing slash). Resolution order: CIPHERBOND_HUB_URL, then HUB_URL, then the
// loopback default. The value is validated; a malformed URL is a hard error so the
// bridge fails loudly rather than sending traffic somewhere unexpected.
func ResolveHubURL() (string, error) {
	raw := firstNonEmpty(os.Getenv(EnvHubURL), os.Getenv(EnvHubURLFallback), DefaultHubURL)
	return normalizeBaseURL(raw)
}

// NormalizeHubURL validates and canonicalises an explicit Hub base URL, such as
// one passed on the command line. It returns the same form ResolveHubURL would.
func NormalizeHubURL(raw string) (string, error) {
	return normalizeBaseURL(raw)
}

// AnthropicBaseURL is the value the bridge injects as ANTHROPIC_BASE_URL: the Hub
// base URL with the /anthropic prefix appended.
func AnthropicBaseURL(hubURL string) string {
	return strings.TrimSuffix(hubURL, "/") + AnthropicPrefix
}

// ControlHealthURL derives the control-plane health endpoint to probe.
//
//   - If CipherBond_HUB_CONTROL_URL is set, it is used verbatim (with /healthz
//     appended only when it carries no path of its own).
//   - Otherwise it is the Hub host on the control port with the /healthz path. The
//     control port is 8384 for the default proxy port (8383); for a custom proxy
//     port it is best-effort derived as proxyPort+1, and operators are encouraged
//     to set CipherBond_HUB_CONTROL_URL explicitly for non-standard topologies.
func ControlHealthURL(hubURL string) (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvControlURL)); override != "" {
		return normalizeControlURL(override)
	}

	u, err := url.Parse(hubURL)
	if err != nil {
		return "", fmt.Errorf("parse hub URL %q: %w", hubURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("hub URL %q has no host", hubURL)
	}

	controlPort := ControlPort
	if p := u.Port(); p != "" && p != DefaultProxyPort {
		if n, convErr := strconv.Atoi(p); convErr == nil {
			controlPort = strconv.Itoa(n + 1)
		}
	}
	return fmt.Sprintf("%s://%s:%s%s", u.Scheme, host, controlPort, HealthPath), nil
}

// Timeout returns the health-check timeout from CipherBond_TIMEOUT (milliseconds),
// falling back to DefaultTimeout for an unset, non-numeric, or non-positive value.
func Timeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(EnvTimeout))
	if raw == "" {
		return DefaultTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return DefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// ClaudeBin returns the CLAUDE_BIN override, or "" if the binary should be resolved
// from PATH.
func ClaudeBin() string {
	return strings.TrimSpace(os.Getenv(EnvClaudeBin))
}

// normalizeBaseURL validates and canonicalises a Hub base URL.
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid hub URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("hub URL %q must start with http:// or https://", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("hub URL %q has no host", raw)
	}
	path := strings.TrimSuffix(u.Path, "/")
	return u.Scheme + "://" + u.Host + path, nil
}

// normalizeControlURL validates an explicit control-URL override and ensures it
// carries a health path.
func normalizeControlURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid %s %q: %w", EnvControlURL, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%s %q must start with http:// or https://", EnvControlURL, raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s %q has no host", EnvControlURL, raw)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = HealthPath
	}
	return u.String(), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
