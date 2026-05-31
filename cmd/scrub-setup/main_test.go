package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseShell(t *testing.T) {
	for _, ok := range []string{"bash", "ZSH", " fish ", "PowerShell"} {
		if _, err := parseShell(ok); err != nil {
			t.Errorf("parseShell(%q) unexpected error: %v", ok, err)
		}
	}
	if _, err := parseShell("tcsh"); err == nil {
		t.Error("parseShell(tcsh) should fail")
	}
}

func TestDetectShell(t *testing.T) {
	tests := []struct {
		goos, shellEnv, override string
		want                     Shell
		wantErr                  bool
	}{
		{goos: "windows", want: PowerShell},
		{goos: "linux", shellEnv: "/bin/zsh", want: Zsh},
		{goos: "linux", shellEnv: "/usr/bin/fish", want: Fish},
		{goos: "linux", shellEnv: "/bin/bash", want: Bash},
		{goos: "linux", shellEnv: "", want: Bash},
		{goos: "darwin", shellEnv: "/bin/sh", want: Bash},
		{goos: "linux", shellEnv: "/bin/bash", override: "powershell", want: PowerShell},
		{goos: "linux", override: "nonsense", wantErr: true},
	}
	for _, tt := range tests {
		got, err := detectShell(tt.goos, tt.shellEnv, tt.override)
		if tt.wantErr {
			if err == nil {
				t.Errorf("detectShell(%q,%q,%q) expected error", tt.goos, tt.shellEnv, tt.override)
			}
			continue
		}
		if err != nil {
			t.Errorf("detectShell(%q,%q,%q) error: %v", tt.goos, tt.shellEnv, tt.override, err)
		}
		if got != tt.want {
			t.Errorf("detectShell(%q,%q,%q) = %q, want %q", tt.goos, tt.shellEnv, tt.override, got, tt.want)
		}
	}
}

func TestProfilePath(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	tests := []struct {
		name string
		sh   Shell
		goos string
		vars map[string]string
		want string
	}{
		{"bash linux", Bash, "linux", map[string]string{"HOME": "/home/u"}, filepath.Join("/home/u", ".bashrc")},
		{"bash darwin", Bash, "darwin", map[string]string{"HOME": "/home/u"}, filepath.Join("/home/u", ".bash_profile")},
		{"zsh zdotdir", Zsh, "linux", map[string]string{"HOME": "/home/u", "ZDOTDIR": "/z"}, filepath.Join("/z", ".zshrc")},
		{"zsh default", Zsh, "linux", map[string]string{"HOME": "/home/u"}, filepath.Join("/home/u", ".zshrc")},
		{"fish xdg", Fish, "linux", map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/x"}, filepath.Join("/x", "fish", "config.fish")},
		{"fish default", Fish, "linux", map[string]string{"HOME": "/home/u"}, filepath.Join("/home/u", ".config", "fish", "config.fish")},
		{"powershell windows", PowerShell, "windows", map[string]string{"USERPROFILE": `C:\Users\u`}, filepath.Join(`C:\Users\u`, "Documents", "PowerShell", "profile.ps1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := profilePath(tt.sh, tt.goos, env(tt.vars))
			if err != nil {
				t.Fatalf("profilePath error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("profilePath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderBlock(t *testing.T) {
	const url = "http://hub.test:8383"

	bash := renderBlock(Bash, url, true)
	mustContain(t, bash, markerBegin, markerEnd, `export SCRUBADUBBER_HUB_URL="http://hub.test:8383"`, "alias claude='scrub-claude'")

	ps := renderBlock(PowerShell, url, true)
	mustContain(t, ps, `$env:SCRUBADUBBER_HUB_URL = "http://hub.test:8383"`, "function claude { scrub-claude @args }")

	fish := renderBlock(Fish, url, true)
	mustContain(t, fish, `set -gx SCRUBADUBBER_HUB_URL "http://hub.test:8383"`, "alias claude 'scrub-claude'")

	noAlias := renderBlock(Bash, url, false)
	if strings.Contains(noAlias, "alias claude") {
		t.Errorf("renderBlock with aliasClaude=false should not contain a claude alias:\n%s", noAlias)
	}
	// The block must not end with a trailing newline (upsertBlock relies on this).
	if strings.HasSuffix(bash, "\n") {
		t.Error("renderBlock output should not end with a newline")
	}
}

func TestUpsertBlock(t *testing.T) {
	blockA := renderBlock(Bash, "http://a:8383", true)
	blockB := renderBlock(Bash, "http://b:8383", true)

	// Append into a file with pre-existing content.
	existing := "# user config\nexport FOO=bar\n"
	out := upsertBlock(existing, blockA)
	mustContain(t, out, "# user config", "export FOO=bar", "http://a:8383")

	// Idempotent: applying the same block again changes nothing.
	if again := upsertBlock(out, blockA); again != out {
		t.Errorf("upsertBlock not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, again)
	}

	// Updating the URL replaces the block in place (no duplicate block).
	updated := upsertBlock(out, blockB)
	if strings.Contains(updated, "http://a:8383") {
		t.Errorf("old URL should be gone:\n%s", updated)
	}
	if strings.Count(updated, markerBegin) != 1 {
		t.Errorf("expected exactly one managed block, got %d:\n%s", strings.Count(updated, markerBegin), updated)
	}
	mustContain(t, updated, "# user config", "export FOO=bar", "http://b:8383")
}

func TestApplyToProfile(t *testing.T) {
	dir := t.TempDir()
	// Nested path verifies parent-directory creation.
	path := filepath.Join(dir, "nested", "config.fish")
	block := renderBlock(Fish, "http://hub:8383", true)

	if err := applyToProfile(path, block); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "http://hub:8383") {
		t.Fatalf("profile missing block:\n%s", got)
	}

	// Idempotent second apply leaves the file byte-for-byte identical.
	before := got
	if err := applyToProfile(path, block); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if after := readFile(t, path); after != before {
		t.Fatalf("second apply changed the file:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestApplyToProfile_BackupsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bashrc")
	original := "# my precious config\nexport KEEP=1\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applyToProfile(path, renderBlock(Bash, "http://hub:8383", true)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if bak := readFile(t, path+".bak"); bak != original {
		t.Fatalf("backup should equal the original; got:\n%s", bak)
	}
	got := readFile(t, path)
	mustContain(t, got, "# my precious config", "export KEEP=1", "http://hub:8383")
}

// --- helpers -----------------------------------------------------------------

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("expected to find %q in:\n%s", sub, s)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}
