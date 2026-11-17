// Package system wraps side effects on the host so they can be faked in tests
// and in dry-run mode on a development machine.
package system

import (
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
)

// Runner executes host commands.
type Runner interface {
	Run(name string, args ...string) error
	// RunW runs a command streaming its combined output to w (used by Tasks).
	RunW(w io.Writer, name string, args ...string) error
	// RunEnvW runs a command with extra environment variables (secrets such as BORG_PASSPHRASE), streaming output to w.
	RunEnvW(w io.Writer, env []string, dir, name string, args ...string) error
	// RunIn runs a command feeding stdin (keeps secrets and SQL out of argv).
	RunIn(stdin string, name string, args ...string) error
	// Output runs a command and returns its stdout. On failure it returns whatever stdout was produced plus an
	// error carrying stderr, because tools such as dpkg-query and fail2ban-client print useful output and still exit non-zero.
	Output(name string, args ...string) (string, error)
}

// ExecRunner runs real commands.
type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (ExecRunner) Output(name string, args ...string) (string, error) {
	var out, errb strings.Builder
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (d *DryRunner) Output(name string, args ...string) (string, error) {
	return "", d.Run(name, args...)
}

func (ExecRunner) RunW(w io.Writer, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = w, w
	c.Env = append(c.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (d *DryRunner) RunW(w io.Writer, name string, args ...string) error {
	fmt.Fprintf(w, "[dry-run] %s %s\n", name, strings.Join(args, " "))
	return d.Run(name, args...)
}

func (ExecRunner) RunIn(stdin, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdin = strings.NewReader(stdin)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DryRunner) RunIn(stdin, name string, args ...string) error {
	d.mu.Lock()
	d.Stdin = append(d.Stdin, stdin)
	d.mu.Unlock()
	return d.Run(name, args...)
}

func (ExecRunner) RunEnvW(w io.Writer, env []string, dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr, c.Dir = w, w, dir
	c.Env = append(c.Environ(), env...)
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (d *DryRunner) RunEnvW(w io.Writer, env []string, dir, name string, args ...string) error {
	fmt.Fprintf(w, "[dry-run] %s %s\n", name, strings.Join(args, " "))
	return d.Run(name, args...)
}

// DryRunner logs and records commands without executing them.
type DryRunner struct {
	mu    sync.Mutex
	Calls []string
	Stdin []string
}

func (d *DryRunner) Run(name string, args ...string) error {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	d.mu.Lock()
	d.Calls = append(d.Calls, line)
	d.mu.Unlock()
	slog.Info("dry-run", "cmd", line)
	return nil
}
