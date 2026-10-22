// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var everything = Project{
	Name:       "sample",
	Components: []Component{Haskell, Kotlin, IntelliJ, VSCode},
	Pins: []Pin{
		{Name: "tala", Location: "https://github.com/Progmasoft/tala"},
		{Name: "talafmt", Location: "https://github.com/Progmasoft/talafmt"},
	},
}

const (
	talaCommit    = "45cc3c365e21818b257b8ef622ca35cb8283e766"
	talafmtCommit = "7bd7d20aa4723fc3111f016731434851451dddda"
	otherCommit   = "0123456789abcdef0123456789abcdef01234567"
)

func write(t *testing.T, root, path, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

// checkout writes a repository with every version file at version.
func checkout(t *testing.T, version string) string {
	root := t.TempDir()
	write(t, root, "sample.cabal", "cabal-version: 3.0\nname: sample\nversion: "+version+"\nbuild-type: Simple\n")
	write(t, root, "build.gradle.kts", "plugins {}\n\nversion = \""+version+"\"\n")
	write(t, root, "intellij/build.gradle.kts", "group = \"x\"\nversion = \""+version+"\"\r\n")
	write(t, root, "vscode/package.json", "{\n  \"name\": \"sample\",\n  \"version\": \""+version+"\",\n  \"devDependencies\": {\n    \"typescript\": \"6.0.3\"\n  }\n}\n")
	write(t, root, "vscode/package-lock.json", "{\n  \"name\": \"sample\",\n  \"version\": \""+version+"\",\n  \"lockfileVersion\": 3,\n  \"packages\": {\n    \"\": {\n      \"name\": \"sample\",\n      \"version\": \""+version+"\",\n      \"license\": \"MPL-2.0\"\n    },\n    \"node_modules/typescript\": {\n      \"version\": \"6.0.3\"\n    }\n  }\n}\n")
	write(t, root, "cabal.project", "packages: sample.cabal\n\n-- The compiler.\nsource-repository-package\n    type: git\n    location: https://github.com/Progmasoft/tala\n    tag: "+talaCommit+"\n    subdir:\n        Talac/Haskell/Syntax\n\nsource-repository-package\n    type: git\n    location: https://github.com/Progmasoft/talafmt.git\n    tag: "+talafmtCommit+"\n\ntests: True\n")
	write(t, root, "CHANGELOG.md", "# Changelog\n\n## Unreleased\n\n- A new rule.\n\n## 0.1.0 - 2026-10-18\n\nThe first release.\n\n- One.\n")
	return root
}

func TestParseVersion(t *testing.T) {
	for _, good := range []string{"0.1.0", "1.20.300", "0.0.0"} {
		if version, err := ParseVersion(good); err != nil || version.String() != good {
			t.Errorf("%s: %v %v", good, version, err)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "v1.2.3", "talafmt-0.1.0", "1.2.3-rc1", " 1.2.3"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestParseComponents(t *testing.T) {
	talafmt := Project{Name: "talafmt", Components: []Component{Haskell, Kotlin}}
	all, err := talafmt.ParseComponents(nil)
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
	selected, err := talafmt.ParseComponents([]string{"Kotlin", "kotlin", "haskell"})
	if err != nil || len(selected) != 2 || selected[0] != Kotlin || selected[1] != Haskell {
		t.Fatal(selected, err)
	}
	if _, err := talafmt.ParseComponents([]string{"vscode"}); err == nil || !strings.Contains(err.Error(), "haskell, kotlin") {
		t.Fatal(err)
	}
}

func TestFindRootFromNestedDirectory(t *testing.T) {
	root := checkout(t, "0.1.0")
	nested := filepath.Join(root, "vscode")
	found, err := everything.FindRoot(nested)
	if err != nil || found != root {
		t.Fatal(found, err)
	}
	other := t.TempDir()
	write(t, other, "cabal.project", "packages: other.cabal\n")
	if _, err := everything.FindRoot(other); err == nil {
		t.Fatal("a checkout of another package was accepted")
	}
}

func TestVersionsAgreeAndChangeTogether(t *testing.T) {
	root := checkout(t, "0.1.0")
	statements, err := everything.ReadVersions(root)
	if err != nil || len(statements) != 6 {
		t.Fatal(statements, err)
	}
	version, err := everything.CurrentVersion(root)
	if err != nil || version.String() != "0.1.0" {
		t.Fatal(version, err)
	}
	changed, err := everything.SetVersion(root, Version{0, 2, 0})
	if err != nil || len(changed) != 5 {
		t.Fatal(changed, err)
	}
	if version, err := everything.CurrentVersion(root); err != nil || version.String() != "0.2.0" {
		t.Fatal(version, err)
	}
	if !strings.Contains(read(t, root, "intellij/build.gradle.kts"), "version = \"0.2.0\"\r\n") {
		t.Fatal("the CRLF line lost its ending")
	}
	lock := read(t, root, "vscode/package-lock.json")
	if strings.Count(lock, "\"version\": \"0.2.0\"") != 2 || !strings.Contains(lock, "\"version\": \"6.0.3\"") {
		t.Fatal(lock)
	}
	if !strings.Contains(read(t, root, "vscode/package.json"), "\"typescript\": \"6.0.3\"") {
		t.Fatal("a dependency version changed")
	}
	if changed, err := everything.SetVersion(root, Version{0, 2, 0}); err != nil || len(changed) != 0 {
		t.Fatal(changed, err)
	}
}

func TestDisagreeingVersionsAreNamed(t *testing.T) {
	root := checkout(t, "0.1.0")
	write(t, root, "build.gradle.kts", "version = \"0.1.1\"\n")
	_, err := everything.CurrentVersion(root)
	if err == nil || !strings.Contains(err.Error(), "build.gradle.kts states 0.1.1") {
		t.Fatal(err)
	}
	write(t, root, "build.gradle.kts", "plugins {}\n")
	if _, err := everything.ReadVersions(root); err == nil || !strings.Contains(err.Error(), "states no version") {
		t.Fatal(err)
	}
	before := read(t, root, "sample.cabal")
	if _, err := everything.SetVersion(root, Version{1, 0, 0}); err == nil {
		t.Fatal("a file without a version was accepted")
	}
	if read(t, root, "sample.cabal") != before {
		t.Fatal("a failed SetVersion changed a file")
	}
}

func TestReleaseNotes(t *testing.T) {
	root := checkout(t, "0.1.0")
	notes, err := ReleaseNotes(root, Version{0, 1, 0})
	if err != nil || notes != "The first release.\n\n- One.\n" {
		t.Fatalf("%q %v", notes, err)
	}
	if _, err := ReleaseNotes(root, Version{0, 2, 0}); err == nil {
		t.Fatal("a missing section was accepted")
	}
}

func TestStartReleaseMovesUnreleasedNotes(t *testing.T) {
	root := checkout(t, "0.1.0")
	today := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	started, err := StartRelease(root, Version{0, 2, 0}, today)
	if err != nil || !started {
		t.Fatal(started, err)
	}
	want := "# Changelog\n\n## Unreleased\n\n## 0.2.0 - 2026-10-22\n\n- A new rule.\n\n## 0.1.0 - 2026-10-18\n"
	if text := read(t, root, "CHANGELOG.md"); !strings.HasPrefix(text, want) {
		t.Fatalf("%q", text)
	}
	if notes, err := ReleaseNotes(root, Version{0, 2, 0}); err != nil || notes != "- A new rule.\n" {
		t.Fatalf("%q %v", notes, err)
	}
	if started, err := StartRelease(root, Version{0, 2, 0}, today); err != nil || started {
		t.Fatal("a released version was started again", err)
	}
	if _, err := StartRelease(root, Version{0, 3, 0}, today); err == nil || !strings.Contains(err.Error(), "nothing under") {
		t.Fatal(err)
	}
}

func TestPins(t *testing.T) {
	root := checkout(t, "0.1.0")
	pins, err := everything.ReadPins(root)
	if err != nil || len(pins) != 2 || pins[0].Commit != talaCommit || pins[1].Commit != talafmtCommit {
		t.Fatal(pins, err)
	}
	pin, err := everything.FindPin("TALA")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := everything.SetPin(root, pin, otherCommit); err != nil || !changed {
		t.Fatal(changed, err)
	}
	text := read(t, root, "cabal.project")
	if !strings.Contains(text, "tag: "+otherCommit+"\n    subdir:") || !strings.Contains(text, "tag: "+talafmtCommit) {
		t.Fatal(text)
	}
	if changed, err := everything.SetPin(root, pin, otherCommit); err != nil || changed {
		t.Fatal("an unchanged pin was rewritten", err)
	}
	if _, err := everything.SetPin(root, pin, "main"); err == nil {
		t.Fatal("a branch name was accepted")
	}
	if _, err := everything.FindPin("catch3"); !errors.Is(err, ErrNoPin) {
		t.Fatal(err)
	}
	write(t, root, "cabal.project", "source-repository-package\n    type: git\n    location: https://github.com/Progmasoft/tala\n    tag: main\n")
	if _, err := everything.ReadPins(root); err == nil || !strings.Contains(err.Error(), "not a full commit") {
		t.Fatal(err)
	}
}

func TestCurrentProjectIsConsistent(t *testing.T) {
	if Current.Name == "" || len(Current.Components) == 0 || Current.Components[0] != Haskell {
		t.Fatal(Current)
	}
	for _, pin := range Current.Pins {
		if !strings.HasPrefix(pin.Location, "https://github.com/Progmasoft/") {
			t.Fatal(pin)
		}
	}
}
