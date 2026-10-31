// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

// Package project describes the repository the helpers serve. The Tala tool
// repositories share every helper source except current.go, which states the
// tool's name, its components and the repositories its cabal.project pins.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Component is one part of the repository that has its own build.
type Component string

const (
	// Haskell is the Cabal package: library, executable and tests.
	Haskell Component = "haskell"
	// Kotlin is the Gradle build of the Kotlin configuration model.
	Kotlin Component = "kotlin"
	// IntelliJ is the IntelliJ Platform plugin below intellij/.
	IntelliJ Component = "intellij"
	// VSCode is the VS Code extension below vscode/.
	VSCode Component = "vscode"
)

// Pin is a repository that cabal.project takes a package from through a
// source-repository-package block, until the package is on Hackage.
type Pin struct {
	// Name is the repository name below github.com/Progmasoft.
	Name string
	// Location is the URL the block names.
	Location string
}

// Project is the description of one tool repository.
type Project struct {
	// Name is the Cabal package, the repository and the executable.
	Name string
	// Components are the parts this repository builds, Haskell first.
	Components []Component
	// Pins are the repositories cabal.project takes packages from.
	Pins []Pin
}

// Has reports whether the repository has the component.
func (p Project) Has(component Component) bool {
	for _, candidate := range p.Components {
		if candidate == component {
			return true
		}
	}
	return false
}

// CabalFile is the package description at the root of the repository.
func (p Project) CabalFile() string {
	return p.Name + ".cabal"
}

// TagPrefix is the prefix of the release tags: talafmt-0.1.0.
func (p Project) TagPrefix() string {
	return p.Name + "-"
}

// ParseComponents turns component names into components of this project. An
// empty list means every component.
func (p Project) ParseComponents(names []string) ([]Component, error) {
	if len(names) == 0 {
		return append([]Component(nil), p.Components...), nil
	}
	var selected []Component
	seen := map[Component]bool{}
	for _, name := range names {
		component := Component(strings.ToLower(strings.TrimSpace(name)))
		if !p.Has(component) {
			return nil, fmt.Errorf("unknown component %q; %s has %s", name, p.Name, p.ComponentList())
		}
		if !seen[component] {
			seen[component] = true
			selected = append(selected, component)
		}
	}
	return selected, nil
}

// ComponentList names the components for messages: "haskell, kotlin".
func (p Project) ComponentList() string {
	names := make([]string, len(p.Components))
	for index, component := range p.Components {
		names[index] = string(component)
	}
	return strings.Join(names, ", ")
}

// FindRoot walks up from start to the checkout of this repository: the first
// directory that holds both cabal.project and the project's Cabal file. The
// helpers therefore work from any directory inside the checkout.
func (p Project) FindRoot(start string) (string, error) {
	directory, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if regular(filepath.Join(directory, "cabal.project")) && regular(filepath.Join(directory, p.CabalFile())) {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("no %s checkout (cabal.project and %s) above %s", p.Name, p.CabalFile(), start)
		}
		directory = parent
	}
}

// FindRootFromWorkingDirectory is FindRoot from the current directory.
func (p Project) FindRootFromWorkingDirectory() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot read the current directory: %w", err)
	}
	return p.FindRoot(directory)
}

func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// ErrNoPin reports a repository that cabal.project does not pin.
var ErrNoPin = errors.New("not pinned")

// FindPin returns the pin of the named repository.
func (p Project) FindPin(name string) (Pin, error) {
	for _, pin := range p.Pins {
		if strings.EqualFold(pin.Name, name) {
			return pin, nil
		}
	}
	names := make([]string, len(p.Pins))
	for index, pin := range p.Pins {
		names[index] = pin.Name
	}
	return Pin{}, fmt.Errorf("%s is %w by %s; the pins are %s", name, ErrNoPin, p.Name, strings.Join(names, ", "))
}
