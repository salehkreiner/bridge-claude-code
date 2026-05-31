package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/salehkreiner/bridge-claude-code/internal/config"
)

// Shell is one of the shells scrub-setup knows how to configure.
type Shell string

const (
	Bash       Shell = "bash"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
)

// Marker lines delimit the block scrub-setup manages. They are comments in every
// supported shell ('#' works for bash, zsh, fish, and PowerShell), so re-running
// setup can find and replace exactly its own block and nothing else.
const (
	markerBegin = "# >>> scrubadubber bridge >>>"
	markerEnd   = "# <<< scrubadubber bridge <<<"
)

// parseShell validates a --shell value.
func parseShell(s string) (Shell, error) {
	switch Shell(strings.ToLower(strings.TrimSpace(s))) {
	case Bash:
		return Bash, nil
	case Zsh:
		return Zsh, nil
	case Fish:
		return Fish, nil
	case PowerShell:
		return PowerShell, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want bash, zsh, fish, or powershell)", s)
	}
}

// detectShell chooses a shell: an explicit override wins; otherwise Windows means
// PowerShell, and on Unix we read the basename of $SHELL, defaulting to bash.
func detectShell(goos, shellEnv, override string) (Shell, error) {
	if strings.TrimSpace(override) != "" {
		return parseShell(override)
	}
	if goos == "windows" {
		return PowerShell, nil
	}
	base := filepath.Base(shellEnv)
	switch {
	case strings.Contains(base, "zsh"):
		return Zsh, nil
	case strings.Contains(base, "fish"):
		return Fish, nil
	default:
		return Bash, nil
	}
}

// profilePath computes the shell profile file scrub-setup should edit. getenv is
// injected so the logic is testable without touching the real environment.
func profilePath(sh Shell, goos string, getenv func(string) string) (string, error) {
	home := getenv("HOME")
	if home == "" {
		home = getenv("USERPROFILE")
	}

	switch sh {
	case Bash:
		// macOS login shells read ~/.bash_profile; elsewhere ~/.bashrc is usual.
		if goos == "darwin" {
			return filepath.Join(home, ".bash_profile"), nil
		}
		return filepath.Join(home, ".bashrc"), nil
	case Zsh:
		if z := getenv("ZDOTDIR"); z != "" {
			return filepath.Join(z, ".zshrc"), nil
		}
		return filepath.Join(home, ".zshrc"), nil
	case Fish:
		cfg := getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		return filepath.Join(cfg, "fish", "config.fish"), nil
	case PowerShell:
		if goos == "windows" {
			docs := filepath.Join(getenv("USERPROFILE"), "Documents")
			return filepath.Join(docs, "PowerShell", "profile.ps1"), nil
		}
		return filepath.Join(home, ".config", "powershell", "profile.ps1"), nil
	default:
		return "", fmt.Errorf("unknown shell %q", sh)
	}
}

// renderBlock produces the marker-delimited configuration block for a shell. The
// returned string starts at markerBegin and ends at markerEnd (no trailing
// newline) so upsertBlock can splice it in cleanly.
func renderBlock(sh Shell, hubURL string, aliasClaude bool) string {
	var lines []string
	lines = append(lines, markerBegin)

	switch sh {
	case Fish:
		lines = append(lines, fmt.Sprintf("set -gx %s %q", config.EnvHubURL, hubURL))
		if aliasClaude {
			lines = append(lines, "alias claude 'scrub-claude'")
		}
	case PowerShell:
		lines = append(lines, fmt.Sprintf("$env:%s = %q", config.EnvHubURL, hubURL))
		if aliasClaude {
			lines = append(lines, "function claude { scrub-claude @args }")
		}
	default: // Bash, Zsh — POSIX syntax
		lines = append(lines, fmt.Sprintf("export %s=%q", config.EnvHubURL, hubURL))
		if aliasClaude {
			lines = append(lines, "alias claude='scrub-claude'")
		}
	}

	lines = append(lines, markerEnd)
	return strings.Join(lines, "\n")
}

// upsertBlock inserts block into existing, replacing any previously written block
// (matched by the markers) in place. It is idempotent: applying the same block
// twice yields the same result, and applying a block with a new URL updates the
// existing block rather than appending a second one.
func upsertBlock(existing, block string) string {
	if begin := strings.Index(existing, markerBegin); begin >= 0 {
		if rel := strings.Index(existing[begin:], markerEnd); rel >= 0 {
			afterEnd := begin + rel + len(markerEnd)
			return existing[:begin] + block + existing[afterEnd:]
		}
	}

	var b strings.Builder
	b.WriteString(existing)
	if len(existing) > 0 {
		if !strings.HasSuffix(existing, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(block)
	b.WriteString("\n")
	return b.String()
}

// reloadHint tells the user how to load the new config into their current shell.
func reloadHint(sh Shell, path string) string {
	if sh == PowerShell {
		return ". '" + path + "'"
	}
	return "source " + path
}
