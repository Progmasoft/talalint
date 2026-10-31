// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

// Command repo-info produces a read-only, secret-free report of the checkout:
// branch, revision, version, pins and the tools on PATH.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Progmasoft/talalint/helpers/internal/project"
	"github.com/spf13/cobra"
)

type toolInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Available bool   `json:"available"`
}

type pinInfo struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
}

type report struct {
	Project      string     `json:"project"`
	Components   []string   `json:"components"`
	Root         string     `json:"root"`
	OS           string     `json:"os"`
	Architecture string     `json:"architecture"`
	Version      string     `json:"version,omitempty"`
	VersionError string     `json:"versionError,omitempty"`
	Pins         []pinInfo  `json:"pins,omitempty"`
	PinError     string     `json:"pinError,omitempty"`
	Branch       string     `json:"branch,omitempty"`
	Revision     string     `json:"revision,omitempty"`
	Dirty        bool       `json:"dirty"`
	GitError     string     `json:"gitError,omitempty"`
	Tools        []toolInfo `json:"tools"`
}

type probe interface {
	LookPath(string) (string, error)
	Git(context.Context, string, ...string) (string, error)
}

type systemProbe struct{}

func (systemProbe) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (systemProbe) Git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	// Never report process environment, remote URLs, credential helpers, or tokens.
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

// toolNames are the programs the helpers may start, for this project.
func toolNames(p project.Project) []string {
	names := []string{"go", "git", "gh"}
	if p.Has(project.Haskell) {
		names = append(names, "ghcup", "ghc", "cabal", "hlint")
	}
	if p.Has(project.Kotlin) || p.Has(project.IntelliJ) {
		names = append(names, "java")
	}
	if p.Has(project.VSCode) {
		names = append(names, "node", "npm")
	}
	return names
}

func collect(ctx context.Context, p project.Project, root string, probe probe) report {
	result := report{Project: p.Name, Root: root, OS: runtime.GOOS, Architecture: runtime.GOARCH}
	for _, component := range p.Components {
		result.Components = append(result.Components, string(component))
	}
	if version, err := p.CurrentVersion(root); err != nil {
		result.VersionError = err.Error()
	} else {
		result.Version = version.String()
	}
	if pins, err := p.ReadPins(root); err != nil {
		result.PinError = err.Error()
	} else {
		for _, pin := range pins {
			result.Pins = append(result.Pins, pinInfo{pin.Pin.Name, pin.Commit})
		}
	}
	for _, name := range toolNames(p) {
		path, err := probe.LookPath(name)
		result.Tools = append(result.Tools, toolInfo{Name: name, Path: path, Available: err == nil})
	}
	for _, query := range []struct {
		args []string
		set  func(string)
	}{
		{[]string{"branch", "--show-current"}, func(v string) { result.Branch = v }},
		{[]string{"rev-parse", "HEAD"}, func(v string) { result.Revision = v }},
		{[]string{"status", "--porcelain", "--untracked-files=no"}, func(v string) { result.Dirty = v != "" }},
	} {
		value, err := probe.Git(ctx, root, query.args...)
		if err != nil {
			result.GitError = "Git checkout metadata could not be read"
			break
		}
		query.set(value)
	}
	return result
}

func write(out io.Writer, result report) {
	fmt.Fprintf(out, "Project: %s (%s)\nCheckout: %s\nHost: %s/%s\n", result.Project, strings.Join(result.Components, ", "), result.Root, result.OS, result.Architecture)
	if result.VersionError != "" {
		fmt.Fprintf(out, "Version: %s\n", result.VersionError)
	} else {
		fmt.Fprintf(out, "Version: %s\n", result.Version)
	}
	if result.PinError != "" {
		fmt.Fprintf(out, "Pins: %s\n", result.PinError)
	}
	for _, pin := range result.Pins {
		fmt.Fprintf(out, "Pin: %s %s\n", pin.Repository, pin.Commit)
	}
	fmt.Fprintf(out, "Branch: %s\nRevision: %s\nTracked changes: %t\n", result.Branch, result.Revision, result.Dirty)
	if result.GitError != "" {
		fmt.Fprintln(out, result.GitError)
	}
	for _, tool := range result.Tools {
		state := tool.Path
		if !tool.Available {
			state = "not found"
		}
		fmt.Fprintf(out, "%-8s %s\n", tool.Name, state)
	}
}

func run(args []string, out, errOut io.Writer, p project.Project, probe probe) error {
	location := "."
	asJSON := false
	cmd := &cobra.Command{Use: "repo-info", Short: "Report the checkout, its version and pins, and the tools on PATH, without changing anything.", Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true}
	cmd.Flags().StringVar(&location, "root", ".", "location inside the checkout")
	cmd.Flags().BoolVar(&asJSON, "json", false, "write a machine-readable JSON report")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		root, err := p.FindRoot(location)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		result := collect(ctx, p, root, probe)
		if asJSON {
			encoder := json.NewEncoder(out)
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		write(out, result)
		return nil
	}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, project.Current, systemProbe{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
