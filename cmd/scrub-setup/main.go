// Command scrub-setup is the one-time installer for the Scrubadubber bridge. It
// writes the Hub URL and a 'claude -> scrub-claude' alias into the user's shell
// profile (inside a clearly marked, idempotent block), and on Windows persists
// the Hub URL as a user environment variable.
//
// It is deliberately boring and transparent: it prints exactly what it will add
// and where, asks before writing (unless --yes), and never touches anything
// outside its own marker block.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/salehkreiner/bridge-claude-code/internal/config"
	"github.com/salehkreiner/bridge-claude-code/internal/hubclient"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(setupMain(os.Args[1:]))
}

func setupMain(args []string) int {
	fs := flag.NewFlagSet("scrub-setup", flag.ContinueOnError)
	hubFlag := fs.String("hub-url", "", "Hub base URL (default: $SCRUBADUBBER_HUB_URL, else "+config.DefaultHubURL+")")
	shellFlag := fs.String("shell", "", "shell to configure: bash|zsh|fish|powershell (default: auto-detect)")
	yes := fs.Bool("yes", false, "skip confirmation prompts (for non-interactive installs)")
	printOnly := fs.Bool("print", false, "show what would change, but write nothing")
	noSetx := fs.Bool("no-setx", false, "Windows: do not persist the Hub URL with setx")
	noAlias := fs.Bool("no-claude-alias", false, "do not alias 'claude' to scrub-claude")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 1. Resolve and validate the Hub URL.
	hubURL, err := resolveHubURL(*hubFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scrub-setup: %v\n", err)
		return 2
	}

	// 2. Validate Hub connectivity. A failure is a warning, not a hard stop:
	//    setup only writes config, and scrub-claude enforces health at runtime.
	if !checkHub(hubURL) && !*yes && !*printOnly {
		if !confirm("Continue anyway?") {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return 1
		}
	}

	// 3. Decide what to write and where.
	sh, err := detectShell(runtime.GOOS, os.Getenv("SHELL"), *shellFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scrub-setup: %v\n", err)
		return 2
	}
	path, err := profilePath(sh, runtime.GOOS, os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scrub-setup: %v\n", err)
		return 2
	}
	block := renderBlock(sh, hubURL, !*noAlias)
	doSetx := runtime.GOOS == "windows" && !*noSetx

	// 4. Dry run: print and stop.
	if *printOnly {
		fmt.Printf("Shell:   %s\nProfile: %s\n\n%s\n", sh, path, block)
		if doSetx {
			fmt.Printf("\nWould also run: setx %s %q\n", config.EnvHubURL, hubURL)
		}
		return 0
	}

	// 5. Confirm, then write.
	fmt.Fprintf(os.Stderr, "Configuring %s.\nProfile: %s\n\n%s\n\n", sh, path, block)
	if doSetx {
		fmt.Fprintf(os.Stderr, "Will also persist %s via setx.\n\n", config.EnvHubURL)
	}
	if !*yes && !confirm("Write these changes?") {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return 1
	}

	if err := applyToProfile(path, block); err != nil {
		fmt.Fprintf(os.Stderr, "scrub-setup: could not update %s: %v\n", path, err)
		return 1
	}
	if doSetx {
		if err := setUserEnv(config.EnvHubURL, hubURL); err != nil {
			// Non-fatal: the profile still exports it for new shells.
			fmt.Fprintf(os.Stderr, "Warning: setx failed (%v); the profile still sets %s.\n", err, config.EnvHubURL)
		}
	}

	fmt.Printf("Done. Updated %s:\n\n%s\n\nStart a new shell, or run: %s\n", path, block, reloadHint(sh, path))
	return 0
}

// resolveHubURL picks the Hub URL (flag wins over env/default) and normalises it.
func resolveHubURL(flagVal string) (string, error) {
	if strings.TrimSpace(flagVal) != "" {
		return config.NormalizeHubURL(flagVal)
	}
	return config.ResolveHubURL()
}

// checkHub probes the Hub control plane and reports whether it was reachable,
// printing a clear warning if not. It never aborts on its own.
func checkHub(hubURL string) bool {
	controlURL, err := config.ControlHealthURL(hubURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n\n", err)
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout())
	defer cancel()
	if _, err := hubclient.Check(ctx, controlURL); err != nil {
		fmt.Fprintf(os.Stderr, `Warning: could not reach the Hub control plane.
  Health checked: %s
  Error:          %v
The Hub may be down, or its control API may be disabled. Setup can still write
your configuration; scrub-claude will re-check the Hub each time you run it.

`, controlURL, err)
		return false
	}
	return true
}

// applyToProfile writes block into the profile at path, replacing any prior block
// and backing up the original to <path>.bak. Missing files (and parent dirs) are
// created. If the block is already present unchanged, nothing is written.
func applyToProfile(path, block string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	updated := upsertBlock(string(existing), block)
	if string(existing) == updated {
		return nil // already up to date
	}

	if len(existing) > 0 {
		if err := os.WriteFile(path+".bak", existing, 0o644); err != nil {
			return fmt.Errorf("writing backup: %w", err)
		}
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// setUserEnv persists a user-scope environment variable on Windows via setx.
func setUserEnv(name, value string) error {
	out, err := exec.Command("setx", name, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// confirm asks a yes/no question on stderr and reads the answer from stdin.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
