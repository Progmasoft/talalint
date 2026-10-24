// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

// Package development implements the developer command of a Tala tool
// repository: it checks the host, builds, tests, packs and releases the tool
// the way CI does.
package development

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Progmasoft/talalint/helpers/internal/console"
	"github.com/Progmasoft/talalint/helpers/internal/project"
	"github.com/spf13/cobra"
)

// environment is what the commands need from the process. Tests replace it.
type environment struct {
	project project.Project
	runner  runner
	out     io.Writer
	errOut  io.Writer
	// open finds the checkout; it runs only after the command line was read,
	// so an invalid command line never touches the checkout.
	open   func() (workspace, error)
	report func(string, ...any)
	table  func([]string, [][]string)
	today  func() time.Time
}

// Main runs the developer command with the host process streams.
func Main() {
	output := console.New()
	r := systemRunner{console: output}
	env := environment{
		project: project.Current,
		runner:  r,
		out:     os.Stdout,
		errOut:  os.Stderr,
		open:    func() (workspace, error) { return newWorkspace(project.Current, r) },
		report:  output.Success,
		table:   output.Table,
		today:   time.Now,
	}
	err := run(os.Args[1:], env)
	output.Summary()
	if err != nil {
		fmt.Fprintln(os.Stderr)
		output.Failure("%v", err)
		os.Exit(1)
	}
}

func run(arguments []string, env environment) error {
	command := newCommand(env)
	command.SetArgs(arguments)
	return command.Execute()
}

// forwarded returns the arguments after --, and refuses any before it.
func forwarded(command *cobra.Command, arguments []string, receiver string) ([]string, error) {
	dash := command.ArgsLenAtDash()
	if dash < 0 {
		if len(arguments) != 0 {
			return nil, fmt.Errorf("%s takes no positional arguments; options for %s go after --", command.Name(), receiver)
		}
		return nil, nil
	}
	if dash != 0 {
		return nil, fmt.Errorf("%s takes no positional arguments; options for %s go after --", command.Name(), receiver)
	}
	return arguments, nil
}

func newCommand(env environment) *cobra.Command {
	p := env.project
	root := &cobra.Command{
		Use:   "develop",
		Short: "Developer command of " + p.Name,
		Long: "Developer command of " + p.Name + `

Checks the host, builds, tests, packs and releases the tool with the commands
CI uses. Run it from anywhere inside the checkout.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetOut(env.out)
	root.SetErr(env.errOut)
	root.CompletionOptions.DisableDefaultCmd = true

	withWorkspace := func(action func(workspace, *cobra.Command, []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, arguments []string) error {
			w, err := env.open()
			if err != nil {
				return err
			}
			return action(w, cmd, arguments)
		}
	}

	var asJSON bool
	doctor := &cobra.Command{
		Use:   "doctor",
		Short: "Report the tools this host has and the versions CI uses.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(p, env.runner, env.out, env.table, asJSON)
		},
	}
	doctor.Flags().BoolVar(&asJSON, "json", false, "write the report as JSON")
	root.AddCommand(doctor)

	componentHelp := "build only this component (" + p.ComponentList() + "); repeat for several"
	for _, spec := range []struct {
		name, short string
		until       stage
	}{
		{"build", "Build every component.", stageBuild},
		{"test", "Build every component and run its tests.", stageTest},
	} {
		var components []string
		command := &cobra.Command{
			Use:   spec.name + " [--component <name>]... [-- <Cabal options>]",
			Short: spec.short,
			RunE: func(cmd *cobra.Command, arguments []string) error {
				extra, err := forwarded(cmd, arguments, "Cabal")
				if err != nil {
					return err
				}
				selected, err := p.ParseComponents(components)
				if err != nil {
					return err
				}
				w, err := env.open()
				if err != nil {
					return err
				}
				return w.runComponents(selected, spec.until, extra)
			},
		}
		command.Flags().StringArrayVar(&components, "component", nil, componentHelp)
		root.AddCommand(command)
	}

	var checkComponents []string
	check := &cobra.Command{
		Use:   "check [--component <name>]...",
		Short: "Run every gate CI runs: version files, pins, builds, tests and package checks.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selected, err := p.ParseComponents(checkComponents)
			if err != nil {
				return err
			}
			w, err := env.open()
			if err != nil {
				return err
			}
			if err := w.runCheck(selected, env.report); err != nil {
				return err
			}
			env.report("every check passed")
			return nil
		},
	}
	check.Flags().StringArrayVar(&checkComponents, "component", nil, componentHelp)
	root.AddCommand(check)

	root.AddCommand(&cobra.Command{
		Use:   "run [-- <" + p.Name + " arguments>]",
		Short: "Build " + p.Name + " and run it with the arguments after --.",
		RunE: func(cmd *cobra.Command, arguments []string) error {
			extra, err := forwarded(cmd, arguments, p.Name)
			if err != nil {
				return err
			}
			w, err := env.open()
			if err != nil {
				return err
			}
			return w.runTool(extra)
		},
	})

	var bundleOutput string
	bundle := &cobra.Command{
		Use:   "bundle [--output <directory>]",
		Short: "Build, stage, smoke-test and archive " + p.Name + " for this host.",
		Long: "Build, stage, smoke-test and archive " + p.Name + ` for this host.

The executable is staged in <output>/` + p.Name + `-<version>-<os>-<arch>/ with
the license, the README and the changelog, run with --version, and packed into
a .zip (Windows) or a .tar.gz with a .sha256 file beside it. The archive
entries carry the commit time, so a commit packs into the same bytes.`,
		Args: cobra.NoArgs,
		RunE: withWorkspace(func(w workspace, _ *cobra.Command, _ []string) error {
			result, err := w.bundle(bundleOutput, env.report)
			if err != nil {
				return err
			}
			env.report("%s", result.Archive)
			env.report("SHA-256 %s", result.Checksum)
			return nil
		}),
	}
	bundle.Flags().StringVar(&bundleOutput, "output", "", "directory for the bundle (default: dist in the checkout)")
	root.AddCommand(bundle)

	var coverageComponents []string
	coverage := &cobra.Command{
		Use:   "coverage [--component haskell|kotlin]...",
		Short: "Run the tests with coverage and print where the HTML reports are.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selected, err := p.ParseComponents(coverageComponents)
			if err != nil {
				return err
			}
			var measured []project.Component
			for _, component := range selected {
				if component == project.Haskell || component == project.Kotlin {
					measured = append(measured, component)
				} else if len(coverageComponents) != 0 {
					return fmt.Errorf("%s has no coverage report; use haskell or kotlin", component)
				}
			}
			w, err := env.open()
			if err != nil {
				return err
			}
			reports, err := w.coverage(measured)
			if err != nil {
				return err
			}
			if len(reports) == 0 {
				return errors.New("the tests ran, but no coverage report was written")
			}
			for _, report := range reports {
				env.report("%s", report)
			}
			return nil
		},
	}
	coverage.Flags().StringArrayVar(&coverageComponents, "component", nil, "measure only this component; repeat for both")
	root.AddCommand(coverage)

	var formatCheck bool
	format := &cobra.Command{
		Use:   "format [--check]",
		Short: "Format the Kotlin sources with Spotless, or check that they are formatted.",
		Args:  cobra.NoArgs,
		RunE: withWorkspace(func(w workspace, _ *cobra.Command, _ []string) error {
			return w.format(formatCheck)
		}),
	}
	format.Flags().BoolVar(&formatCheck, "check", false, "only report sources that are not formatted")
	root.AddCommand(format)

	root.AddCommand(&cobra.Command{
		Use:   "lint [-- <HLint options>]",
		Short: "Run HLint over the Haskell sources.",
		RunE: func(cmd *cobra.Command, arguments []string) error {
			extra, err := forwarded(cmd, arguments, "HLint")
			if err != nil {
				return err
			}
			w, err := env.open()
			if err != nil {
				return err
			}
			return w.lint(extra)
		},
	})

	var cleanAll, cleanDryRun bool
	clean := &cobra.Command{
		Use:   "clean [--all] [--dry-run]",
		Short: "Remove the build output from the checkout.",
		Long: `Remove the build output from the checkout: dist-newstyle, dist and the
Gradle and npm build folders. With --all the downloaded caches go as well
(.gradle, the IntelliJ Platform and node_modules); the next build fetches them
again. Sources and untracked work are never touched.`,
		Args: cobra.NoArgs,
		RunE: withWorkspace(func(w workspace, _ *cobra.Command, _ []string) error {
			return w.clean(cleanAll, cleanDryRun, env.report)
		}),
	}
	clean.Flags().BoolVar(&cleanAll, "all", false, "remove the downloaded caches too")
	clean.Flags().BoolVar(&cleanDryRun, "dry-run", false, "only report what would be removed")
	root.AddCommand(clean)

	var setVersion string
	version := &cobra.Command{
		Use:   "version [<major.minor.patch>] [--set <major.minor.patch>]",
		Short: "Show the version, check a release, or set a new version.",
		Long: `Show the version, check a release, or set a new version.

Without arguments it prints the version every file states and fails when
they disagree. With a version it checks that the checkout is ready to be
tagged as ` + p.TagPrefix() + `<version>: every file states it, the changelog has its
notes and the tag is new. --set writes the version into every file and turns
the Unreleased notes of the changelog into its section.`,
		Args: cobra.MaximumNArgs(1),
		RunE: withWorkspace(func(w workspace, _ *cobra.Command, arguments []string) error {
			switch {
			case setVersion != "" && len(arguments) != 0:
				return errors.New("give either a version to check or --set, not both")
			case setVersion != "":
				parsed, err := project.ParseVersion(setVersion)
				if err != nil {
					return err
				}
				return w.setVersion(parsed, env.today(), env.report)
			case len(arguments) == 1:
				parsed, err := project.ParseVersion(arguments[0])
				if err != nil {
					return err
				}
				return w.checkRelease(parsed, env.report)
			}
			return w.showVersion(env.out)
		}),
	}
	version.Flags().StringVar(&setVersion, "set", "", "write this version into every version file")
	root.AddCommand(version)

	var notesOutput string
	notes := &cobra.Command{
		Use:   "release-notes <major.minor.patch> [--output <file>]",
		Short: "Print the changelog section of a version, for the release page.",
		Args:  cobra.ExactArgs(1),
		RunE: withWorkspace(func(w workspace, _ *cobra.Command, arguments []string) error {
			parsed, err := project.ParseVersion(arguments[0])
			if err != nil {
				return err
			}
			text, err := project.ReleaseNotes(w.root, parsed)
			if err != nil {
				return err
			}
			if notesOutput != "" {
				return os.WriteFile(notesOutput, []byte(text), 0o644)
			}
			_, err = io.WriteString(env.out, text)
			return err
		}),
	}
	notes.Flags().StringVar(&notesOutput, "output", "", "write the notes to this file")
	root.AddCommand(notes)

	// A repository without source-repository-package pins has nothing for
	// pin to show or move.
	if len(p.Pins) > 0 {
		root.AddCommand(newPinCommand(env, withWorkspace))
	}
	return root
}

func newPinCommand(
	env environment,
	withWorkspace func(func(workspace, *cobra.Command, []string) error) func(*cobra.Command, []string) error,
) *cobra.Command {
	pinNames := make([]string, len(env.project.Pins))
	for index, pin := range env.project.Pins {
		pinNames[index] = pin.Name
	}
	return &cobra.Command{
		Use:   "pin [<repository> <commit|tag|branch>]",
		Short: "Show or change the commits cabal.project takes packages from.",
		Long: `Show or change the commits cabal.project takes packages from.

Without arguments it lists every source-repository-package pin. With a
repository (` + strings.Join(pinNames, ", ") + `) and a reference it points that pin at
the commit; a tag or a branch is resolved to its commit with git ls-remote,
because a pin must stay reproducible.`,
		Args: func(cmd *cobra.Command, arguments []string) error {
			if len(arguments) != 0 && len(arguments) != 2 {
				return errors.New("pin takes no arguments, or a repository and a reference")
			}
			return nil
		},
		RunE: withWorkspace(func(w workspace, _ *cobra.Command, arguments []string) error {
			if len(arguments) == 0 {
				return w.showPins(env.out)
			}
			return w.updatePin(arguments[0], arguments[1], env.report)
		}),
	}
}
