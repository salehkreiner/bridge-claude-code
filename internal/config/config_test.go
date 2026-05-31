package config

import (
	"testing"
	"time"
)

// clearHubEnv blanks every Hub-related variable for a hermetic test. t.Setenv
// restores the prior values when the test ends.
func clearHubEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvHubURL, EnvHubURLFallback, EnvControlURL, EnvTimeout, EnvClaudeBin} {
		t.Setenv(k, "")
	}
}

func TestResolveHubURL(t *testing.T) {
	tests := []struct {
		name     string
		primary  string
		fallback string
		want     string
		wantErr  bool
	}{
		{name: "default when unset", want: DefaultHubURL},
		{name: "primary wins", primary: "http://hub.example:8383", fallback: "http://other:8383", want: "http://hub.example:8383"},
		{name: "fallback used", fallback: "http://hub.example:8383", want: "http://hub.example:8383"},
		{name: "trailing slash trimmed", primary: "http://127.0.0.1:8383/", want: "http://127.0.0.1:8383"},
		{name: "https accepted", primary: "https://hub.corp", want: "https://hub.corp"},
		{name: "custom path preserved", primary: "http://gw.corp/scrub/", want: "http://gw.corp/scrub"},
		{name: "missing scheme rejected", primary: "127.0.0.1:8383", wantErr: true},
		{name: "wrong scheme rejected", primary: "ftp://hub:8383", wantErr: true},
		{name: "garbage rejected", primary: "::::", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearHubEnv(t)
			t.Setenv(EnvHubURL, tt.primary)
			t.Setenv(EnvHubURLFallback, tt.fallback)

			got, err := ResolveHubURL()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnthropicBaseURL(t *testing.T) {
	tests := map[string]string{
		"http://127.0.0.1:8383":  "http://127.0.0.1:8383/anthropic",
		"http://127.0.0.1:8383/": "http://127.0.0.1:8383/anthropic",
		"https://hub.corp":       "https://hub.corp/anthropic",
	}
	for in, want := range tests {
		if got := AnthropicBaseURL(in); got != want {
			t.Errorf("AnthropicBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestControlHealthURL(t *testing.T) {
	tests := []struct {
		name    string
		hubURL  string
		control string // SCRUBADUBBER_HUB_CONTROL_URL override
		want    string
		wantErr bool
	}{
		{name: "default port maps to 8384", hubURL: "http://127.0.0.1:8383", want: "http://127.0.0.1:8384/healthz"},
		{name: "no port assumes control 8384", hubURL: "http://hub.corp", want: "http://hub.corp:8384/healthz"},
		{name: "https preserved", hubURL: "https://hub.corp:8383", want: "https://hub.corp:8384/healthz"},
		{name: "custom port derives plus one", hubURL: "http://hub.corp:9000", want: "http://hub.corp:9001/healthz"},
		{name: "override used and healthz appended", control: "http://control.corp:9999", want: "http://control.corp:9999/healthz"},
		{name: "override with explicit path respected", control: "http://control.corp:9999/status", want: "http://control.corp:9999/status"},
		{name: "override without scheme rejected", control: "control.corp:9999", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearHubEnv(t)
			t.Setenv(EnvControlURL, tt.control)

			got, err := ControlHealthURL(tt.hubURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want time.Duration
	}{
		{name: "default when unset", val: "", want: DefaultTimeout},
		{name: "custom value", val: "500", want: 500 * time.Millisecond},
		{name: "non-numeric falls back", val: "soon", want: DefaultTimeout},
		{name: "zero falls back", val: "0", want: DefaultTimeout},
		{name: "negative falls back", val: "-5", want: DefaultTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearHubEnv(t)
			t.Setenv(EnvTimeout, tt.val)
			if got := Timeout(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaudeBin(t *testing.T) {
	clearHubEnv(t)
	t.Setenv(EnvClaudeBin, "  /opt/claude/claude  ")
	if got := ClaudeBin(); got != "/opt/claude/claude" {
		t.Fatalf("got %q, want trimmed path", got)
	}
}
