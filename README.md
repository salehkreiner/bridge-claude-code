# bridge-claude-code

**A small, auditable bridge that routes Claude Code's outbound API traffic through
the [Scrubadubber Hub](https://github.com/salehkreiner/scrubadubber-hub) before it
reaches Anthropic.** It does this with a single, well-documented mechanism: it sets
the `ANTHROPIC_BASE_URL` environment variable, checks that the Hub is reachable, and
then hands off to your real `claude`. That is the entire program.

The Hub it points at is the **on-device pseudonymization and egress-control layer**:
it replaces sensitive values with reversible pseudonyms before your traffic leaves
the machine and re-injects the real values into responses, with the re-identification
key held locally. Scrubadubber is **free for individuals, enforceable and validated
for organizations** — this bridge is identical either way; an admin just points it at
a shared Hub.

What this bridge **does not** do — and you can confirm every line yourself, because
it has **zero third-party dependencies**:

- It does **not** read, log, store, or modify your prompts, responses, or API keys.
- It does **not** do any pseudonymization, masking, or detection. All of that lives in
  the Hub (a separate repo). This bridge only changes *where Claude Code sends traffic*.
- It does **not** inject code, hook processes, or install drivers. Interception is
  nothing more than the `ANTHROPIC_BASE_URL` variable that Claude Code already honors.
- It does **not** phone home. No analytics, no telemetry. The only network call the
  bridge itself makes is a ~2‑second health check to your Hub.

If the Hub is unreachable, the bridge **refuses to start** rather than quietly
sending your traffic to Anthropic unprotected.

---

## How it works (the data flow)

```
   you run:  claude "explain this file"
       │
       ▼
   ┌──────────────┐  1. resolve the Hub URL
   │ scrub-claude │  2. health-check the Hub (refuse to start if it is down)
   │  (this repo) │  3. set ANTHROPIC_BASE_URL=http://HUB:8383/anthropic
   └──────┬───────┘  4. exec() the real claude — and step out of the way
          │
          │   (the bridge process is now gone; it is NOT in the data path)
          ▼
   ┌──────────────┐  Claude Code sends its normal Anthropic /v1/messages
   │  Claude Code │  requests, just addressed to the Hub instead of Anthropic.
   └──────┬───────┘
          │   POST http://HUB:8383/anthropic/v1/messages   (x-api-key preserved)
          ▼
   ┌──────────────┐  The Hub is the ONLY place pseudonymization happens. It
   │ Scrubadubber │  then forwards the request upstream.
   │     Hub      │
   └──────┬───────┘
          │   POST https://api.anthropic.com/v1/messages
          ▼
   ┌──────────────┐
   │   Anthropic  │
   └──────────────┘
```

The most important property: **after the handoff, the bridge no longer exists as a
process.** `scrub-claude` calls `exec()` (on Windows, it runs `claude` as a child and
mirrors its exit code), so your prompt and response bytes travel directly from Claude
Code to the Hub. They never pass through the bridge.

---

## Quick start

### Individual developer

You run Claude Code locally and a Hub on your own machine (default `127.0.0.1:8383`).

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.ps1 | iex
```

The installer downloads the binaries, puts them on your `PATH`, and runs
`scrub-setup`, which adds a small marked block to your shell profile:

```sh
# >>> scrubadubber bridge >>>
export SCRUBADUBBER_HUB_URL="http://127.0.0.1:8383"
alias claude='scrub-claude'
# <<< scrubadubber bridge <<<
```

Open a new shell and keep working exactly as before — `claude` is now transparently
protected. (Prefer to keep `claude` unaliased? Re-run `scrub-setup --no-claude-alias`
and call `scrub-claude` explicitly.)

### Enterprise team

A central Hub runs on a company server; every developer points their bridge at it.
Set the Hub URL once and run the installer:

```sh
SCRUBADUBBER_HUB_URL="http://hub.internal.example:8383" \
  curl -fsSL https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.sh | sh
```

No code changes are required to target a shared Hub — only the `SCRUBADUBBER_HUB_URL`
environment variable.

### Manual install (build from source)

```sh
git clone https://github.com/salehkreiner/bridge-claude-code
cd bridge-claude-code
go build -o ~/.local/bin/scrub-claude ./cmd/scrub-claude
go build -o ~/.local/bin/scrub-setup  ./cmd/scrub-setup
scrub-setup            # writes your shell profile (asks before it does)
```

---

## Configuration

All configuration is via environment variables — there is no config file.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SCRUBADUBBER_HUB_URL` | `http://127.0.0.1:8383` | Base URL of the Hub's Anthropic proxy. |
| `HUB_URL` | (unset) | Fallback used only if `SCRUBADUBBER_HUB_URL` is unset. |
| `SCRUBADUBBER_HUB_CONTROL_URL` | derived | Full URL of the Hub health endpoint. Override for non-standard topologies; otherwise derived as the Hub host on port `8384` at `/healthz`. |
| `SCRUBADUBBER_TIMEOUT` | `2000` | Health-check timeout, in milliseconds. |
| `CLAUDE_BIN` | (from `PATH`) | Path to the real `claude` binary. |

`scrub-setup` flags: `--hub-url <url>`, `--shell <bash\|zsh\|fish\|powershell>`,
`--yes` (non-interactive), `--print` (dry run), `--no-setx` (Windows: skip the
persistent env var), `--no-claude-alias`.

---

## Security

This repository is meant to be read. It is deliberately tiny and dependency-free.

- **No data is stored or transformed by the bridge.** `scrub-claude` sets one
  environment variable and `exec()`s `claude`. Your request/response bytes never
  flow through the bridge process. (`scrub-setup` writes only your shell profile,
  only inside its marked block, and only when you run it — backing up the original
  to `<profile>.bak` first.)
- **One outbound connection, and you can see it.** The only network call the bridge
  makes is `GET http://<hub>:8384/healthz`. After that, all traffic is Claude Code
  talking to the Hub directly.
- **No redirects are followed** on the health check, so the bridge can never be
  bounced to an unexpected host.
- **Your API key is untouched.** The bridge does not read or log `x-api-key` /
  `ANTHROPIC_API_KEY`; Claude Code sends it as usual and the Hub forwards it upstream.
- **Fail-closed.** If the Hub control plane does not answer, the bridge prints an
  actionable error and exits non-zero. It never falls back to talking to Anthropic
  directly. (Honest caveat: if you invoke `claude` *without* the bridge/alias, you
  bypass protection; and the health gate requires the Hub's control API to be on —
  see Troubleshooting.)
- **Reproducible, verifiable builds.** Release binaries are built with
  `CGO_ENABLED=0` and published with a `SHA256SUMS` file; the install scripts verify
  checksums when a SHA-256 tool is available. You can also just build from source.

Exit codes the bridge uses when it refuses to run (otherwise `claude`'s own exit
code is passed through unchanged):

| Code | Meaning |
| --- | --- |
| `69` | Hub control plane unreachable. |
| `78` | Bad configuration (e.g. malformed Hub URL). |
| `126` | Found `claude` but could not execute it. |
| `127` | Could not find `claude`. |

---

## Verifying it works

**Confirm `ANTHROPIC_BASE_URL` is being injected** (Hub must be running so the health
check passes). `scrub-claude` passes its arguments to whatever `CLAUDE_BIN` points
at, so you can borrow a command that just prints the environment:

```sh
# macOS / Linux
CLAUDE_BIN="$(command -v printenv)" scrub-claude ANTHROPIC_BASE_URL
# -> http://127.0.0.1:8383/anthropic
```

```powershell
# Windows (PowerShell)
$env:CLAUDE_BIN = "cmd.exe"; scrub-claude /c "echo %ANTHROPIC_BASE_URL%"
# -> http://127.0.0.1:8383/anthropic
```

**Confirm traffic flows through the Hub.** Watch the Hub's logs (or its request
counter) while you issue a prompt through `scrub-claude` (or `claude`, if aliased).
You should see the request arrive at the Hub's `/anthropic` endpoint. If you see
nothing at the Hub but Claude Code still works, traffic is going straight to
Anthropic — check that `ANTHROPIC_BASE_URL` is set as above.

---

## Troubleshooting

**"cannot reach the Scrubadubber Hub control plane"** — the bridge health-checks
`http://<hub>:8384/healthz` and it did not answer. Either the Hub is not running, the
URL is wrong, or the Hub's control API is disabled. The `/healthz` endpoint is served
only when both `review.enabled` and `review.control_api.enabled` are set in the Hub's
configuration. Start the Hub, fix `SCRUBADUBBER_HUB_URL`, or enable the control API.

**"could not find the 'claude' binary on PATH"** — install Claude Code, or set
`CLAUDE_BIN` to the full path of your `claude` executable.

**`claude` still goes straight to Anthropic** — you are probably calling `claude`
without the alias. Re-run `scrub-setup`, open a new shell, or call `scrub-claude`
directly. Verify the alias with `type claude` (Unix) or `Get-Command claude`
(PowerShell).

**`scrub-claude` not found after install** — its install directory is not on your
`PATH` yet. Open a new shell, or add `~/.local/bin` (Unix) /
`%LOCALAPPDATA%\scrubadubber\bin` (Windows) to `PATH`.

**Windows asks for administrator rights to run `scrub-setup`** — you should not see
this. `scrub-setup.exe` ships with an embedded manifest declaring `asInvoker`, so it
runs with your normal user rights and never needs elevation. If a prompt appears, you
are almost certainly running an old build from before this fix — reinstall the latest
release. (Background: Windows' UAC "Installer Detection" tries to elevate any
unmanifested executable whose name contains "setup"; the manifest disables that.)

---

## How interception works (the one technical detail)

Claude Code sends its API requests to the URL in `ANTHROPIC_BASE_URL`, defaulting to
`https://api.anthropic.com`. The Hub speaks the standard Anthropic protocol on
`http://<hub>:8383/anthropic`. Setting `ANTHROPIC_BASE_URL` to that address is the
*entire* interception mechanism. There is no magic — no proxy injected into Claude
Code, no patched binary, no system-wide network hook.

---

## Building and testing

```sh
make build     # build both binaries into ./dist
make test      # go test ./...
make vet       # go vet ./...
make fmt-check # fail if anything is not gofmt-clean
make cross     # cross-compile the full release matrix into ./dist
```

CI builds, vets, and tests on Linux, macOS, and Windows. Tagging `vX.Y.Z`
cross-compiles `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, and `windows/amd64` and
publishes them to GitHub Releases with checksums.

---

## License

[Apache-2.0](LICENSE).
