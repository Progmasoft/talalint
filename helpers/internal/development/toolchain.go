// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package development

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

// The toolchain CI builds with. GHC and Cabal are the versions the workflow
// installs; the others are the lowest versions the builds accept.
const (
	wantedGHC     = "9.10.3"
	wantedCabal   = "3.16"
	wantedGo      = "1.26"
	wantedJava    = "25"
	wantedNode    = "24"
	prebuildHint  = "go run ./helpers/cmd/prebuild install"
	versionNumber = `[0-9]+(?:\.[0-9]+)*`
)

var versionInOutput = regexp.MustCompile(versionNumber)

// tool is one program a component needs.
type tool struct {
	name        string
	program     string
	versionArgs []string
	// wanted describes the version CI uses, for the doctor's table.
	wanted string
	// accept decides a found version: "" when it is fine, otherwise why not.
	accept func(found string) string
	// fatal makes an unaccepted version an error rather than a warning.
	fatal bool
	// components are those that need the tool; none means every one.
	components []project.Component
	optional   bool
	purpose    string
}

func atLeast(minimum string) func(string) string {
	return func(found string) string {
		if compareVersions(found, minimum) < 0 {
			return "older than " + minimum
		}
		return ""
	}
}

func exactly(wanted string) func(string) string {
	return func(found string) string {
		if found != wanted {
			return "CI builds with " + wanted
		}
		return ""
	}
}

// compareVersions compares dotted numbers; a missing part counts as zero.
func compareVersions(left, right string) int {
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < max(len(a), len(b)); index++ {
		var x, y int
		if index < len(a) {
			x, _ = strconv.Atoi(a[index])
		}
		if index < len(b) {
			y, _ = strconv.Atoi(b[index])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// tools lists every program the helpers may need, in the doctor's order.
func tools() []tool {
	return []tool{
		{name: "Git", program: "git", versionArgs: []string{"--version"}, wanted: "any", accept: atLeast("0"), fatal: true, purpose: "checkout and the pinned repositories"},
		{name: "Go", program: "go", versionArgs: []string{"version"}, wanted: wantedGo + " or newer", accept: atLeast(wantedGo), fatal: true, purpose: "these helpers"},
		{name: "GHC", program: "ghc", versionArgs: []string{"--numeric-version"}, wanted: wantedGHC, accept: exactly(wantedGHC), components: []project.Component{project.Haskell}, purpose: "the Haskell package"},
		{name: "Cabal", program: "cabal", versionArgs: []string{"--numeric-version"}, wanted: wantedCabal + " or newer", accept: atLeast(wantedCabal), fatal: true, components: []project.Component{project.Haskell}, purpose: "the Haskell package"},
		{name: "Java", program: "java", versionArgs: []string{"--version"}, wanted: wantedJava + " or newer", accept: atLeast(wantedJava), fatal: true, components: []project.Component{project.Kotlin, project.IntelliJ}, purpose: "Gradle and the Kotlin builds"},
		{name: "Node.js", program: "node", versionArgs: []string{"--version"}, wanted: wantedNode + " or newer", accept: atLeast(wantedNode), components: []project.Component{project.VSCode}, purpose: "the VS Code extension"},
		{name: "npm", program: "npm", versionArgs: []string{"--version"}, wanted: "any", accept: atLeast("0"), fatal: true, components: []project.Component{project.VSCode}, purpose: "the VS Code extension"},
		{name: "HLint", program: "hlint", versionArgs: []string{"--numeric-version"}, wanted: "any", accept: atLeast("0"), components: []project.Component{project.Haskell}, optional: true, purpose: "develop lint"},
	}
}

// needed reports whether the project needs the tool for the components.
func (t tool) needed(selected []project.Component) bool {
	if len(t.components) == 0 {
		return true
	}
	for _, component := range t.components {
		for _, candidate := range selected {
			if component == candidate {
				return true
			}
		}
	}
	return false
}

// toolState is what the doctor found for one tool.
type toolState struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Version  string `json:"version,omitempty"`
	Wanted   string `json:"wanted"`
	State    string `json:"state"`
	Note     string `json:"note,omitempty"`
	Optional bool   `json:"optional"`
	Purpose  string `json:"purpose"`
}

const (
	stateReady   = "ready"
	stateWarning = "warning"
	stateMissing = "missing"
	stateFailed  = "unsuitable"
)

// examine finds and versions every tool the components need.
func examine(r runner, selected []project.Component) []toolState {
	var states []toolState
	for _, candidate := range tools() {
		if !candidate.needed(selected) {
			continue
		}
		state := toolState{Name: candidate.name, Wanted: candidate.wanted, Optional: candidate.optional, Purpose: candidate.purpose}
		path, err := r.LookPath(candidate.program)
		if err != nil {
			state.State = stateMissing
			states = append(states, state)
			continue
		}
		state.Path = path
		output, err := r.Output("", path, candidate.versionArgs...)
		version := versionInOutput.FindString(strings.TrimPrefix(strings.TrimSpace(output), "go version go"))
		switch {
		case err != nil || version == "":
			state.State = stateFailed
			state.Note = "its version could not be read"
		case candidate.accept(version) != "":
			state.Version = version
			state.Note = candidate.accept(version)
			state.State = stateWarning
			if candidate.fatal {
				state.State = stateFailed
			}
		default:
			state.Version = version
			state.State = stateReady
		}
		states = append(states, state)
	}
	return states
}

// problems turns the states into the error the doctor and the builds return.
func problems(states []toolState) error {
	var missing []string
	for _, state := range states {
		if state.Optional {
			continue
		}
		switch state.State {
		case stateMissing:
			missing = append(missing, state.Name+" is missing")
		case stateFailed:
			missing = append(missing, fmt.Sprintf("%s %s is unsuitable: %s", state.Name, state.Version, state.Note))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s; run %s", strings.Join(missing, ", "), prebuildHint)
}

// requireTools checks the tools a command needs before it starts one.
func requireTools(r runner, selected []project.Component) error {
	return problems(examine(r, selected))
}

func runDoctor(p project.Project, r runner, out io.Writer, table func([]string, [][]string), asJSON bool) error {
	states := examine(r, p.Components)
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(struct {
			Project string      `json:"project"`
			Host    string      `json:"host"`
			Tools   []toolState `json:"tools"`
		}{p.Name, runtime.GOOS + "/" + runtime.GOARCH, states}); err != nil {
			return err
		}
		return problems(states)
	}
	fmt.Fprintf(out, "%s on %s/%s, components: %s\n\n", p.Name, runtime.GOOS, runtime.GOARCH, p.ComponentList())
	rows := make([][]string, 0, len(states))
	for _, state := range states {
		found := state.Version
		if found == "" {
			found = "-"
		}
		result := state.State
		if state.Note != "" {
			result += " (" + state.Note + ")"
		}
		if state.Optional && state.State == stateMissing {
			result = "optional, missing"
		}
		rows = append(rows, []string{state.Name, found, state.Wanted, result, state.Purpose})
	}
	table([]string{"Tool", "Found", "Wanted", "State", "For"}, rows)
	if err := problems(states); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nThe host can build every component.")
	return nil
}
