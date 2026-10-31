// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package development

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

// stage is how far a command takes a component.
type stage int

const (
	// stageBuild compiles.
	stageBuild stage = iota
	// stageTest compiles and runs the tests.
	stageTest
	// stageCheck does what CI does: the tests and every other gate.
	stageCheck
)

// workspace is the checkout a command works in, with what it needs to start
// the builds there.
type workspace struct {
	project project.Project
	root    string
	runner  runner
	goos    string
	getenv  func(string) string
}

func (w workspace) cabal(arguments ...string) error {
	return w.runner.Run(w.root, cabalEnvironment(w.goos, w.getenv), "cabal", arguments...)
}

// gradle runs the Gradle wrapper of the checkout. It is started by its path:
// the wrapper is not on PATH, and on Windows it is the batch file.
func (w workspace) gradle(arguments ...string) error {
	wrapper := filepath.Join(w.root, "gradlew")
	if w.goos == "windows" {
		wrapper += ".bat"
	}
	return w.runner.Run(w.root, nil, wrapper, arguments...)
}

func (w workspace) npm(arguments ...string) error {
	return w.runner.Run(filepath.Join(w.root, "vscode"), nil, "npm", arguments...)
}

// runComponent takes one component to the stage. The commands are those of
// the CI workflow, so a green local check predicts a green CI run.
func (w workspace) runComponent(component project.Component, until stage, extra []string) error {
	switch component {
	case project.Haskell:
		if err := w.cabal(append([]string{"build", "all"}, extra...)...); err != nil {
			return err
		}
		if until >= stageTest {
			if err := w.cabal(append([]string{"test", "all", "--test-show-details=direct"}, extra...)...); err != nil {
				return err
			}
		}
		if until >= stageCheck {
			return w.cabal("check")
		}
		return nil
	case project.Kotlin:
		switch until {
		case stageBuild:
			return w.gradle("assemble")
		case stageTest:
			return w.gradle("check")
		}
		// Spotless, the compiler with warnings as errors, every test and
		// the coverage report CI uploads.
		return w.gradle("check", "jacocoTestReport")
	case project.IntelliJ:
		tasks := []string{"-p", "intellij", "compileKotlin", "processResources"}
		if until >= stageCheck {
			tasks = append(tasks, "dokkaGenerateHtml")
		}
		return w.gradle(tasks...)
	case project.VSCode:
		if err := w.npm("ci"); err != nil {
			return err
		}
		if until == stageBuild {
			return w.npm("run", "compile")
		}
		return w.npm("run", "check")
	}
	return fmt.Errorf("unknown component %q", component)
}

// runComponents takes every selected component to the stage, Haskell first,
// and stops at the first failure.
func (w workspace) runComponents(selected []project.Component, until stage, extra []string) error {
	if err := requireTools(w.runner, selected); err != nil {
		return err
	}
	for _, component := range selected {
		var arguments []string
		if component == project.Haskell {
			arguments = extra
		}
		if err := w.runComponent(component, until, arguments); err != nil {
			return fmt.Errorf("%s: %w", component, err)
		}
	}
	return nil
}

// runCheck is what CI checks: the metadata first, because it is fast and a
// wrong version or pin makes the rest pointless, then every component.
func (w workspace) runCheck(selected []project.Component, report func(string, ...any)) error {
	version, err := w.project.CurrentVersion(w.root)
	if err != nil {
		return err
	}
	report("every version file states %s", version)
	pins, err := w.project.ReadPins(w.root)
	if err != nil {
		return err
	}
	for _, pin := range pins {
		report("%s is pinned to %s", pin.Pin.Name, pin.Commit[:12])
	}
	return w.runComponents(selected, stageCheck, nil)
}

// runTool builds the executable and runs it with the arguments.
func (w workspace) runTool(arguments []string) error {
	if err := requireTools(w.runner, []project.Component{project.Haskell}); err != nil {
		return err
	}
	return w.cabal(append([]string{"run", "--verbose=0", "exe:" + w.project.Name, "--"}, arguments...)...)
}

// format applies or checks the Kotlin formatting with Spotless. The Haskell
// sources have no enforced format.
func (w workspace) format(check bool) error {
	if !w.project.Has(project.Kotlin) {
		return fmt.Errorf("%s has no Kotlin sources to format", w.project.Name)
	}
	if err := requireTools(w.runner, []project.Component{project.Kotlin}); err != nil {
		return err
	}
	task := "spotlessApply"
	if check {
		task = "spotlessCheck"
	}
	return w.gradle(task)
}

// lint runs HLint over the Haskell sources.
func (w workspace) lint(extra []string) error {
	if _, err := w.runner.LookPath("hlint"); err != nil {
		return fmt.Errorf("hlint is not on PATH; install it with `cabal install hlint` and rerun")
	}
	var directories []string
	for _, candidate := range []string{"sources", "app"} {
		if info, err := os.Stat(filepath.Join(w.root, candidate)); err == nil && info.IsDir() {
			directories = append(directories, candidate)
		}
	}
	return w.runner.Run(w.root, nil, "hlint", append(directories, extra...)...)
}

func newWorkspace(p project.Project, r runner) (workspace, error) {
	root, err := p.FindRootFromWorkingDirectory()
	if err != nil {
		return workspace{}, err
	}
	return workspace{project: p, root: root, runner: r, goos: runtime.GOOS, getenv: os.Getenv}, nil
}
