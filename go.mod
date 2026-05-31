// bridge-claude-code — the public, auditable Scrubadubber bridge for Claude Code.
//
// This module intentionally declares ZERO third-party dependencies. The entire
// point of this repo is that a security reviewer can read every line and confirm
// there is no keylogging, no exfiltration, and no payload mutation. Adding a
// dependency would mean adding code an auditor must also trust. Don't.
//
// If you are about to add a `require` line: stop. Everything the bridge needs is
// in the Go standard library.
module github.com/salehkreiner/bridge-claude-code

go 1.24
