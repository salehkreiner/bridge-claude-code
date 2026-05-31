//go:build !windows

package main

import "syscall"

// handoff replaces the current process with claude using execve(2). On success it
// never returns: there is no wrapper process left in the middle, so the
// controlling terminal, signal handling, and exit code all belong to claude
// directly. It only returns when the exec itself fails.
func handoff(claudePath string, args, env []string) (int, error) {
	argv := append([]string{claudePath}, args...)
	err := syscall.Exec(claudePath, argv, env)
	return 0, err
}
