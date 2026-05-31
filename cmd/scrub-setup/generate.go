package main

// rsrc_windows_amd64.syso embeds an application manifest into scrub-setup.exe so
// that Windows does not force a UAC elevation prompt on launch (its Installer
// Detection heuristic treats any unmanifested "*setup*" binary as an installer).
// The Go linker picks up the *_windows_amd64.syso automatically; no build-time
// tooling is needed. The committed .syso is regenerated only when the manifest
// changes:
//
//go:generate go run ../../tools/winmanifest -manifest scrub-setup.manifest -out rsrc_windows_amd64.syso
