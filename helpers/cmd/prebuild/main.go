// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

// prebuild installs the system tools a development host of this repository
// needs. Cabal, Gradle and npm remain the owners of their builds afterwards.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/Progmasoft/talalint/helpers/internal/console"
	"github.com/Progmasoft/talalint/helpers/internal/project"
	"github.com/spf13/cobra"
)

// The GHC and Cabal CI builds with; prebuild installs these, not GHCup's
// recommendation of the day, so a local build matches CI.
const (
	pinnedGHC   = "9.10.3"
	pinnedCabal = "3.16.1.0"
)

type bootstrapHost int

const (
	bootstrapUnsupported bootstrapHost = iota
	bootstrapWindows
	bootstrapMacOS
	bootstrapUbuntu
	bootstrapFedora
	bootstrapDebian
)

type toolRequirement struct {
	name        string
	executables []string
	// components that need the tool; none means every one.
	components      []project.Component
	wingetPackageID string
	homebrewFormula string
	// homebrewCask installs a cask rather than a formula.
	homebrewCask bool
	aptPackages  []string
	dnfPackages  []string
}

var allTools = []toolRequirement{
	{name: "Go", executables: []string{"go"}, wingetPackageID: "GoLang.Go", homebrewFormula: "go", aptPackages: []string{"golang-go"}, dnfPackages: []string{"golang"}},
	{name: "Git", executables: []string{"git"}, wingetPackageID: "Git.Git", homebrewFormula: "git", aptPackages: []string{"git"}, dnfPackages: []string{"git"}},
	{name: "GHCup", executables: []string{"ghcup"}, components: []project.Component{project.Haskell}, homebrewFormula: "ghcup"},
	{name: "Temurin JDK 25", executables: []string{"java"}, components: []project.Component{project.Kotlin, project.IntelliJ}, wingetPackageID: "EclipseAdoptium.Temurin.25.JDK", homebrewFormula: "temurin@25", homebrewCask: true, aptPackages: []string{"openjdk-25-jdk"}, dnfPackages: []string{"java-25-openjdk-devel"}},
	{name: "Node.js", executables: []string{"node", "npm"}, components: []project.Component{project.VSCode}, wingetPackageID: "OpenJS.NodeJS.LTS", homebrewFormula: "node", aptPackages: []string{"nodejs", "npm"}, dnfPackages: []string{"nodejs", "npm"}},
}

// requiredTools are the tools the components of the project need.
func requiredTools(p project.Project) []toolRequirement {
	var tools []toolRequirement
	for _, requirement := range allTools {
		if len(requirement.components) == 0 {
			tools = append(tools, requirement)
			continue
		}
		for _, component := range requirement.components {
			if p.Has(component) {
				tools = append(tools, requirement)
				break
			}
		}
	}
	return tools
}

func main() {
	output := console.New()
	err := runPrebuild(os.Args[1:], bootstrapSystemRunner{console: output})
	output.Summary()
	if err != nil {
		fmt.Fprintln(os.Stderr)
		output.Failure("%v", err)
		os.Exit(1)
	}
}

func usage(p project.Project) string {
	names := make([]string, 0)
	for _, requirement := range requiredTools(p) {
		names = append(names, requirement.name)
	}
	return p.Name + ` development-host bootstrap

Supported hosts are Windows 10/11, macOS 15/26, Ubuntu 26.04 LTS, Fedora 43
and Debian 13. The tools are ` + strings.Join(names, ", ") + `; GHCup then
installs GHC ` + pinnedGHC + ` and Cabal ` + pinnedCabal + `, the versions CI builds with. GHCup
itself comes from a bootstrap script pinned by commit and SHA-256.`
}

// newPrebuildCommand owns the command line. Parsing is separate from host
// detection and installation, so an invalid invocation changes nothing and a
// request for help works on a host the bootstrap does not support.
func newPrebuildCommand(p project.Project, runner bootstrapRunner, output, errorOutput io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "prebuild",
		Short:         "Check or install the tools a " + p.Name + " development host needs.",
		Long:          usage(p),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.SetOut(output)
	root.SetErr(errorOutput)
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		&cobra.Command{
			Use:   "check",
			Short: "Report missing host tools without changing the machine.",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				host, err := detectBootstrapHost(runtime.GOOS)
				if err != nil {
					return err
				}
				return reportBootstrapState(p, host, runner, output)
			},
		},
		&cobra.Command{
			Use:   "install",
			Short: "Install missing tools with winget, Homebrew, apt, or dnf, and GHC and Cabal with GHCup.",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				host, err := detectBootstrapHost(runtime.GOOS)
				if err != nil {
					return err
				}
				if err := installBootstrapTools(p, host, runner, output); err != nil {
					return err
				}
				return reportPostInstallState(p, host, runner, output)
			},
		},
	)
	return root
}

func runPrebuild(arguments []string, runner bootstrapRunner) error {
	command := newPrebuildCommand(project.Current, runner, os.Stdout, os.Stderr)
	command.SetArgs(arguments)
	return command.Execute()
}

func detectBootstrapHost(goos string) (bootstrapHost, error) {
	switch goos {
	case "windows":
		return bootstrapWindows, nil
	case "darwin":
		return bootstrapMacOS, nil
	case "linux":
		contents, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return bootstrapUnsupported, fmt.Errorf("cannot identify the Linux distribution: %w", err)
		}
		return classifyBootstrapLinux(string(contents))
	default:
		return bootstrapUnsupported, fmt.Errorf("%s is not a supported development host", goos)
	}
}

func classifyBootstrapLinux(release string) (bootstrapHost, error) {
	fields := make(map[string]string)
	for _, line := range strings.Split(release, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			fields[key] = strings.Trim(value, `"`)
		}
	}
	switch {
	case fields["ID"] == "ubuntu" && fields["VERSION_ID"] == "26.04":
		return bootstrapUbuntu, nil
	case fields["ID"] == "fedora" && fields["VERSION_ID"] == "43":
		return bootstrapFedora, nil
	case fields["ID"] == "debian" && fields["VERSION_ID"] == "13":
		return bootstrapDebian, nil
	default:
		return bootstrapUnsupported, fmt.Errorf("unsupported Linux release %q %q; expected Ubuntu 26.04 LTS, Fedora 43 or Debian 13", fields["ID"], fields["VERSION_ID"])
	}
}

func missingTools(p project.Project, runner bootstrapRunner) []toolRequirement {
	var missing []toolRequirement
	for _, requirement := range requiredTools(p) {
		for _, executable := range requirement.executables {
			if _, err := runner.LookPath(executable); err != nil {
				missing = append(missing, requirement)
				break
			}
		}
	}
	return missing
}

func containsRequirement(requirements []toolRequirement, name string) bool {
	for _, requirement := range requirements {
		if requirement.name == name {
			return true
		}
	}
	return false
}

func reportBootstrapState(p project.Project, host bootstrapHost, runner bootstrapRunner, out io.Writer) error {
	fmt.Fprintf(out, "Host: %s\n", bootstrapHostName(host))
	missing := missingTools(p, runner)
	for _, requirement := range requiredTools(p) {
		state := "[ready]  "
		if containsRequirement(missing, requirement.name) {
			state = "[missing]"
		}
		fmt.Fprintf(out, "%s %s\n", state, requirement.name)
	}
	incomplete := len(missing) != 0
	if host == bootstrapMacOS {
		if _, err := runner.Output("xcode-select", "-p"); err != nil {
			fmt.Fprintln(out, "[missing] Xcode Command Line Tools")
			incomplete = true
		} else {
			fmt.Fprintln(out, "[ready]   Xcode Command Line Tools")
		}
	}
	if p.Has(project.Haskell) {
		for _, tool := range []struct{ program, name string }{{"ghc", "GHC"}, {"cabal", "Cabal"}} {
			if _, err := runner.LookPath(tool.program); err != nil {
				fmt.Fprintf(out, "[missing] %s selected by GHCup\n", tool.name)
				incomplete = true
			} else {
				fmt.Fprintf(out, "[ready]   %s\n", tool.name)
			}
		}
	}
	if incomplete {
		return errors.New("development host is incomplete; run 'go run ./helpers/cmd/prebuild install'")
	}
	fmt.Fprintln(out, "\nDevelopment host bootstrap is complete; 'go run ./helpers/cmd/develop doctor' compares the versions with CI.")
	return nil
}

func reportPostInstallState(p project.Project, host bootstrapHost, runner bootstrapRunner, out io.Writer) error {
	fmt.Fprintln(out, "\nInstallation commands completed.")
	if err := reportBootstrapState(p, host, runner, out); err != nil {
		fmt.Fprintln(out, "\nNew package-manager PATH entries may not be visible to this process yet.")
		fmt.Fprintln(out, "Open a new terminal, then run:")
		fmt.Fprintln(out, "  go run ./helpers/cmd/prebuild check")
		fmt.Fprintln(out, "  go run ./helpers/cmd/develop doctor")
		return errors.New("installation finished, but verification is incomplete in this process; open a new terminal and run prebuild check")
	}
	return nil
}

func installBootstrapTools(p project.Project, host bootstrapHost, runner bootstrapRunner, out io.Writer) error {
	missing := missingTools(p, runner)
	switch host {
	case bootstrapWindows:
		winget, err := runner.LookPath("winget")
		if err != nil {
			return errors.New("winget is required on Windows; install or repair App Installer first")
		}
		for _, requirement := range missing {
			if requirement.name == "GHCup" {
				if err := installGHCupWindows(runner); err != nil {
					return err
				}
				continue
			}
			if err := installWingetPackage(runner, out, winget, requirement.wingetPackageID); err != nil {
				return fmt.Errorf("cannot install %s: %w", requirement.name, err)
			}
		}
	case bootstrapMacOS:
		brew, err := runner.LookPath("brew")
		if err != nil {
			return errors.New("Homebrew is required on macOS; install it from https://brew.sh and rerun prebuild")
		}
		if _, err := runner.Output("xcode-select", "-p"); err != nil {
			fmt.Fprintln(out, "Requesting the Apple Command Line Tools installer...")
			if err := runner.Run("xcode-select", "--install"); err != nil {
				return fmt.Errorf("cannot request Xcode Command Line Tools: %w", err)
			}
		}
		for _, requirement := range missing {
			if homebrewPackageInstalled(runner, brew, requirement) {
				fmt.Fprintf(out, "Skipping %s; Homebrew reports it is already installed.\n", requirement.name)
				continue
			}
			if err := runner.Run(brew, homebrewInstallArguments(requirement)...); err != nil {
				return fmt.Errorf("cannot install %s: %w", requirement.name, err)
			}
		}
	default:
		if err := installLinuxTools(host, runner, missing); err != nil {
			return err
		}
	}
	if !p.Has(project.Haskell) {
		return nil
	}
	return installHaskellTools(runner, out)
}

func installLinuxTools(host bootstrapHost, runner bootstrapRunner, missing []toolRequirement) error {
	manager := "apt-get"
	// What GHCup's GHC needs to link programs.
	base := []string{"curl", "xz-utils", "libgmp-dev", "libffi-dev", "libncurses-dev", "build-essential"}
	if host == bootstrapFedora {
		manager = "dnf"
		base = []string{"curl", "xz", "gmp-devel", "libffi-devel", "ncurses-devel", "gcc", "make"}
	}
	if _, err := runner.LookPath(manager); err != nil {
		return fmt.Errorf("%s is required on %s", manager, bootstrapHostName(host))
	}
	if host == bootstrapUbuntu || host == bootstrapDebian {
		if err := runLinuxPackageManager(runner, manager, []string{"update"}); err != nil {
			return err
		}
	}
	if err := runLinuxPackageManager(runner, manager, linuxInstallArguments(base)); err != nil {
		return fmt.Errorf("cannot install Linux bootstrap prerequisites: %w", err)
	}
	for _, requirement := range missing {
		if requirement.name == "GHCup" {
			// The reviewed script is hash-verified before it runs and receives no shell-parsed URL.
			if err := installGHCupUnix(runner); err != nil {
				return fmt.Errorf("cannot install GHCup: %w", err)
			}
			continue
		}
		packages := requirement.aptPackages
		if host == bootstrapFedora {
			packages = requirement.dnfPackages
		}
		if len(packages) == 0 {
			return fmt.Errorf("no %s package is defined for %s", requirement.name, bootstrapHostName(host))
		}
		if err := runLinuxPackageManager(runner, manager, linuxInstallArguments(packages)); err != nil {
			return fmt.Errorf("cannot install %s: %w", requirement.name, err)
		}
	}
	return nil
}

func linuxInstallArguments(packages []string) []string {
	return append([]string{"install", "-y"}, packages...)
}

func runLinuxPackageManager(runner bootstrapRunner, manager string, arguments []string) error {
	if os.Geteuid() == 0 {
		return runner.Run(manager, arguments...)
	}
	if _, err := runner.LookPath("sudo"); err != nil {
		return errors.New("sudo is required to install distribution packages as a non-root user")
	}
	return runner.Run("sudo", append([]string{manager}, arguments...)...)
}

func homebrewInstallArguments(requirement toolRequirement) []string {
	if requirement.homebrewCask {
		return []string{"install", "--cask", requirement.homebrewFormula}
	}
	return []string{"install", requirement.homebrewFormula}
}

func homebrewPackageInstalled(runner bootstrapRunner, brew string, requirement toolRequirement) bool {
	if requirement.homebrewFormula == "" {
		return false
	}
	kind := "--formula"
	if requirement.homebrewCask {
		kind = "--cask"
	}
	_, err := runner.Output(brew, "list", kind, requirement.homebrewFormula)
	return err == nil
}

func installWingetPackage(runner bootstrapRunner, out io.Writer, winget string, packageID string) error {
	if packageID == "" {
		return errors.New("package has no winget identifier")
	}
	listing, err := runner.Output(winget, "list", "--id", packageID, "--exact", "--source", "winget", "--disable-interactivity")
	if err == nil && strings.Contains(strings.ToLower(listing), strings.ToLower(packageID)) {
		fmt.Fprintf(out, "Skipping %s; WinGet reports it is already installed.\n", packageID)
		return nil
	}
	return runner.Run(winget, "install", "--id", packageID, "--exact", "--source", "winget",
		"--accept-package-agreements", "--accept-source-agreements", "--silent", "--disable-interactivity")
}

// installHaskellTools installs the GHC and Cabal CI uses when the host has
// none. A host that has another GHC selected keeps it; doctor reports the
// difference.
func installHaskellTools(runner bootstrapRunner, out io.Writer) error {
	ghcup, err := findGHCup(runner)
	if err != nil {
		fmt.Fprintln(out, "GHCup was installed but is not visible yet; open a new terminal and rerun prebuild install.")
		return nil
	}
	for _, tool := range []struct{ program, version string }{{"ghc", pinnedGHC}, {"cabal", pinnedCabal}} {
		if _, err := runner.LookPath(tool.program); err == nil {
			continue
		}
		if err := runner.Run(ghcup, "install", tool.program, tool.version, "--set"); err != nil {
			return fmt.Errorf("cannot install %s %s: %w", tool.program, tool.version, err)
		}
	}
	return nil
}

func bootstrapHostName(host bootstrapHost) string {
	switch host {
	case bootstrapWindows:
		return "Windows 10/11"
	case bootstrapMacOS:
		return "macOS Sequoia/Tahoe"
	case bootstrapUbuntu:
		return "Ubuntu 26.04 LTS"
	case bootstrapFedora:
		return "Fedora 43 (not tested by CI)"
	case bootstrapDebian:
		return "Debian 13 (not tested by CI)"
	default:
		return "unsupported"
	}
}
