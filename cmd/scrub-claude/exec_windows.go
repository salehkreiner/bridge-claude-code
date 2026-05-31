//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// handoff runs claude as a child process and mirrors its exit code. Windows has
// no execve(2), so unlike the Unix path a wrapper process necessarily remains.
//
// claude shares our console, so a Ctrl-C is delivered to it directly. We ignore
// the interrupt in this parent process so we do not die first and lose the child's
// real exit code — we wait for claude to decide how to exit.
func handoff(claudePath string, args, env []string) (int, error) {
	cmd := exec.Command(claudePath, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	signal.Ignore(os.Interrupt)

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}
