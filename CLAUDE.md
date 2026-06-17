# bridge-claude-code â€” Project Context

## What This Repo Is
A public, auditable bridge that routes Claude Code's outbound API traffic through
the CipherBond Hub before it reaches Anthropic's servers. Engineers can read every
line of this repo to confirm there is no keylogging, no data exfiltration, and no
behaviour beyond transparent interception and re-routing.

This is one spoke in the CipherBond architecture. The Hub (repo: CipherBond-hub)
is the **on-device pseudonymization and egress-control layer**: it replaces sensitive
values with reversible pseudonyms before traffic leaves the machine and re-injects the
real values on the way back, with the re-identification key held locally. This bridge
does nothing to the data itself â€” it only ensures Claude Code talks to the Hub instead
of talking directly to Anthropic. (CipherBond is **free for individuals, enforceable
and validated for organizations**; this bridge is the same for both â€” an admin just
points it at a shared Hub.)

## Strict Scope
Build ONLY the bridge. Do not re-implement any scrubbing, masking, or detection logic
here. Do not import or duplicate Hub internals. If something belongs in the Hub, it
stays in the Hub.

## User Profile
Two user types, both must be served:

1. Individual developer â€” runs Claude Code locally on their own machine. Wants
   zero-friction setup: one command, then their normal `claude` workflow is protected
   automatically. They should not have to think about the Hub after setup.

2. Enterprise team â€” a central Hub instance runs on a company server or internal
   network. Many developers on the same team each install the bridge. Each bridge
   points at the shared Hub rather than localhost. An admin sets HUB_URL once;
   developers just run the bridge wrapper.

## How Interception Works (Hub contract)
The Hub listens on port 8383 by default. It expects Anthropic-destined traffic to
arrive at:

    http://<HUB_URL>:8383/anthropic   (plain HTTP, loopback or internal network)

Claude Code respects the ANTHROPIC_BASE_URL environment variable. Setting it to the
above address is the entire interception mechanism â€” no code injection, no process
hooking, no kernel drivers.

The bridge's job is to set that variable correctly and ensure the Hub is reachable
before handing control to claude.

Hub payload contract (do not deviate):
- The Hub expects standard Anthropic /v1/messages JSON payloads, forwarded as-is
- It preserves all headers including x-api-key (forwarded to Anthropic upstream)
- It returns responses in identical Anthropic schema â€” Claude Code sees no difference
- Streaming (SSE) is passed through; response re-injection over SSE is off by default

## Deliverables

### 1. Wrapper binary (cmd/scrub-claude/main.go) â€” written in Go
The primary user-facing artifact. Usage:

    scrub-claude [claude-flags...]

Behaviour:
- Resolve Hub URL: check CIPHERBOND_HUB_URL env var, fall back to
  http://127.0.0.1:8383
- Health-check the Hub: GET <HUB_URL>/health â€” if unreachable, print a clear
  actionable error and exit (never silently pass traffic to Anthropic unprotected)
- Set ANTHROPIC_BASE_URL=<HUB_URL>/anthropic in the child process environment
- Exec the real claude binary (look up PATH, honour CLAUDE_BIN env override)
- Exit with the same exit code claude exits with

The wrapper must be transparent: no banners, no extra output unless an error occurs.
A developer running `scrub-claude` should feel like they are running `claude`.

### 2. Setup command (cmd/scrub-setup/main.go)
One-time installer. Usage:

    scrub-setup [--hub-url <url>] [--shell <bash|zsh|fish|powershell>]

Behaviour:
- Validate Hub connectivity at the given (or default) URL
- Write CIPHERBOND_HUB_URL and a scrub-claude shell alias to the user's shell
  profile (auto-detect shell, confirm before writing)
- On Windows: write to the user's PowerShell profile and optionally set a persistent
  user-level environment variable via setx
- Print a one-line confirmation and the exact lines it added â€” no surprises

### 3. Health-check client (internal/hubclient/health.go)
Shared package used by both binaries:
- GET /health against the Hub control plane (:8384) or proxy port (:8383/health)
- Returns HubStatus{Reachable bool, Version string, ScrubMode string}
- Timeout: 2 seconds â€” never block the developer's workflow

### 4. Configuration (no config file required for basic use)
All configuration is via environment variables only â€” no config file to manage.
Environment variables, in priority order:

    CIPHERBOND_HUB_URL    full base URL of Hub (default: http://127.0.0.1:8383)
    CipherBond_TIMEOUT    health-check timeout in ms (default: 2000)
    CLAUDE_BIN              path to claude binary (default: resolved from PATH)

### 5. Install script (install.sh + install.ps1)
Curl-pipeable installers (the standard open-source pattern):

    # Unix
    curl -fsSL https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.sh | sh

    # Windows PowerShell
    irm https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.ps1 | iex

Each script:
- Downloads the correct pre-built binary for the OS/arch from GitHub Releases
- Places it in ~/.local/bin (Unix) or %LOCALAPPDATA%\CipherBond\bin (Windows)
- Adds that directory to PATH if needed
- Runs scrub-setup automatically on first install

### 6. GitHub Actions CI + Release workflow
- CI: build + vet + test on ubuntu/macos/windows on every push
- Release: on tag v*.*.*, cross-compile binaries for
  linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
  and publish to GitHub Releases (this is what the install scripts download)

### 7. README.md â€” the trust document
This is the most important file in this repo. It must:
- State clearly in the first paragraph what this software does and does not do
- Link to the Hub repo for the pseudonymization / egress-control logic
- Show the complete data flow in plain English (Claude Code â†’ bridge â†’ Hub â†’ Anthropic)
- Include a "Security" section explaining: no data is stored by the bridge, no
  outbound connections except to the Hub and (via Hub) to Anthropic, no analytics
- Include quick-start for individual developer and for enterprise team
- Include verification instructions: how to confirm ANTHROPIC_BASE_URL is set, how
  to confirm traffic is flowing through the Hub (Hub logs)

## Directory Structure

    bridge-claude-code/
    â”œâ”€â”€ cmd/
    â”‚   â”œâ”€â”€ scrub-claude/
    â”‚   â”‚   â””â”€â”€ main.go          # wrapper binary entrypoint
    â”‚   â””â”€â”€ scrub-setup/
    â”‚       â””â”€â”€ main.go          # setup/installer entrypoint
    â”œâ”€â”€ internal/
    â”‚   â””â”€â”€ hubclient/
    â”‚       â””â”€â”€ health.go        # Hub health-check client
    â”œâ”€â”€ .github/
    â”‚   â””â”€â”€ workflows/
    â”‚       â”œâ”€â”€ ci.yml           # build + test on 3 OSes
    â”‚       â””â”€â”€ release.yml      # cross-compile + publish on tag
    â”œâ”€â”€ install.sh               # Unix curl-pipe installer
    â”œâ”€â”€ install.ps1              # Windows irm installer
    â”œâ”€â”€ go.mod / go.sum
    â”œâ”€â”€ Makefile
    â””â”€â”€ README.md                # the trust document

## Design Principles
- Zero dependencies beyond Go stdlib â€” this repo must be trivially auditable
- Never touch the payload â€” the bridge sets an env var and execs; that is all
- Fail loudly and safely â€” if the Hub is unreachable, refuse to start rather than
  silently bypassing protection
- Works on Windows (primary user platform), macOS, and Linux
- Enterprise-ready from day one via HUB_URL env var â€” no code change needed to
  point at a shared Hub

## Hub Alignment Checklist
Before implementing, verify these against the live Hub repo:
- Hub health endpoint path and response schema
- Hub proxy listen port (default 8383) and Anthropic prefix (/anthropic)
- Hub control plane port (default 8384) â€” used for health check fallback
- Hub /health response fields (version, scrub_mode) to populate HubStatus
