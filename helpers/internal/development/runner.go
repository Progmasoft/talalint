// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package development

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/Progmasoft/talalint/helpers/internal/console"
)

// runner starts programs. The commands talk to it rather than to os/exec, so
// their tests see every program a command would start without starting it.
type runner interface {
	// Run starts a program as one step and waits for it.
	Run(directory string, environment []string, name string, arguments ...string) error
	// Output starts a program and returns its standard output, trimmed.
	Output(directory string, name string, arguments ...string) (string, error)
	// LookPath finds a program on PATH.
	LookPath(name string) (string, error)
}

type systemRunner struct {
	console *console.Console
}

func (r systemRunner) Run(directory string, environment []string, name string, arguments ...string) error {
	return r.console.Step(console.Describe(name, arguments), func(stdout, stderr io.Writer) error {
		command := exec.Command(name, arguments...)
		command.Dir = directory
		command.Env = mergeEnvironment(os.Environ(), environment)
		command.Stdin = os.Stdin
		command.Stdout = stdout
		command.Stderr = stderr
		return command.Run()
	})
}

func (systemRunner) Output(directory string, name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	output, err := command.Output()
	return strings.TrimSpace(string(output)), err
}

func (systemRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// mergeEnvironment returns base with the NAME=value entries of overrides
// replacing those of the same name. Windows compares names without case.
func mergeEnvironment(base, overrides []string) []string {
	if len(overrides) == 0 {
		return base
	}
	key := func(entry string) string {
		name, _, _ := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" {
			return strings.ToUpper(name)
		}
		return name
	}
	replaced := map[string]bool{}
	for _, entry := range overrides {
		replaced[key(entry)] = true
	}
	merged := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		if !replaced[key(entry)] {
			merged = append(merged, entry)
		}
	}
	return append(merged, overrides...)
}

// cabalEnvironment is the environment Cabal runs in. On Windows a pinned
// repository can have paths longer than MAX_PATH, which Git refuses to check
// out unless core.longpaths is on; the setting is added through Git's
// environment configuration, after any the developer already set, so neither
// the global configuration nor the checkout is changed.
func cabalEnvironment(goos string, getenv func(string) string) []string {
	if goos != "windows" {
		return nil
	}
	count := 0
	if existing := getenv("GIT_CONFIG_COUNT"); existing != "" {
		parsed, err := strconv.Atoi(existing)
		if err != nil || parsed < 0 {
			// Git would reject the variable itself; leave it for Git to report.
			return nil
		}
		count = parsed
	}
	return []string{
		fmt.Sprintf("GIT_CONFIG_COUNT=%d", count+1),
		fmt.Sprintf("GIT_CONFIG_KEY_%d=core.longpaths", count),
		fmt.Sprintf("GIT_CONFIG_VALUE_%d=true", count),
	}
}

// executableName adds the host's executable extension.
func executableName(goos, name string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}
