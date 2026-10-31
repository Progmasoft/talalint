// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Version is a release number: major.minor.patch.
type Version struct {
	Major, Minor, Patch int
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

// ParseVersion reads major.minor.patch without leading zeros. A prefixed tag
// such as talafmt-0.1.0 is not a version; the prefix belongs to the tag.
func ParseVersion(text string) (Version, error) {
	match := versionPattern.FindStringSubmatch(text)
	if match == nil {
		return Version{}, fmt.Errorf("%q is not a version; expected major.minor.patch, such as 0.1.0", text)
	}
	numbers := [3]int{}
	for index := range numbers {
		numbers[index], _ = strconv.Atoi(match[index+1])
	}
	return Version{numbers[0], numbers[1], numbers[2]}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// VersionFile is one file that states the version, and how.
type VersionFile struct {
	// Path is relative to the checkout, with forward slashes.
	Path string
	// pattern matches the version as its second group, between the first and
	// the third. Every match in the file must state the same version.
	pattern *regexp.Regexp
}

var (
	cabalVersion  = regexp.MustCompile(`(?m)^(version:[ \t]*)([^\s]+)([ \t]*\r?)$`)
	gradleVersion = regexp.MustCompile(`(?m)^(version = ")([^"]*)("[ \t]*\r?)$`)
	// The version of package.json and of the package itself in the lock file
	// are two spaces in; the lock file states it again for the root package.
	npmVersion  = regexp.MustCompile(`(?m)^(  "version": ")([^"]*)(",\r?)$`)
	lockVersion = regexp.MustCompile(`(?s)("packages": \{\s*"": \{[^{}]*?"version": ")([^"]*)(")`)
)

// VersionFiles lists every file that states the version of this project.
func (p Project) VersionFiles() []VersionFile {
	files := []VersionFile{{p.CabalFile(), cabalVersion}}
	if p.Has(Kotlin) {
		files = append(files, VersionFile{"build.gradle.kts", gradleVersion})
	}
	if p.Has(IntelliJ) {
		files = append(files, VersionFile{"intellij/build.gradle.kts", gradleVersion})
	}
	if p.Has(VSCode) {
		files = append(files,
			VersionFile{"vscode/package.json", npmVersion},
			VersionFile{"vscode/package-lock.json", npmVersion},
			VersionFile{"vscode/package-lock.json", lockVersion})
	}
	return files
}

// Statement is the version one file states.
type Statement struct {
	Path    string
	Version string
}

// ReadVersions returns what every version file states. A file that states no
// version, or two different ones, is an error: the release would be ambiguous.
func (p Project) ReadVersions(root string) ([]Statement, error) {
	var statements []Statement
	for _, file := range p.VersionFiles() {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.Path, err)
		}
		matches := file.pattern.FindAllSubmatch(contents, -1)
		if len(matches) == 0 {
			return nil, fmt.Errorf("%s states no version", file.Path)
		}
		first := string(matches[0][2])
		for _, match := range matches[1:] {
			if string(match[2]) != first {
				return nil, fmt.Errorf("%s states two versions, %s and %s", file.Path, first, match[2])
			}
		}
		statements = append(statements, Statement{file.Path, first})
	}
	return statements, nil
}

// CurrentVersion is the version every file states, or an error that names the
// files that disagree.
func (p Project) CurrentVersion(root string) (Version, error) {
	statements, err := p.ReadVersions(root)
	if err != nil {
		return Version{}, err
	}
	var disagreements []string
	for _, statement := range statements[1:] {
		if statement.Version != statements[0].Version {
			disagreements = append(disagreements, fmt.Sprintf("%s states %s", statement.Path, statement.Version))
		}
	}
	if len(disagreements) != 0 {
		return Version{}, fmt.Errorf("the version files disagree: %s states %s, but %s", statements[0].Path,
			statements[0].Version, strings.Join(disagreements, ", "))
	}
	return ParseVersion(statements[0].Version)
}

// SetVersion writes the version into every version file. It reads every file
// first, so a missing one changes nothing.
func (p Project) SetVersion(root string, version Version) ([]string, error) {
	contents := map[string][]byte{}
	var order []string
	for _, file := range p.VersionFiles() {
		path := file.Path
		if _, loaded := contents[path]; !loaded {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
			contents[path] = data
			order = append(order, path)
		}
		if !file.pattern.Match(contents[path]) {
			return nil, fmt.Errorf("%s states no version", path)
		}
		contents[path] = file.pattern.ReplaceAll(contents[path], []byte("${1}"+version.String()+"${3}"))
	}
	var changed []string
	for _, path := range order {
		full := filepath.Join(root, filepath.FromSlash(path))
		before, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(before, contents[path]) {
			continue
		}
		if err := writePreservingMode(full, contents[path]); err != nil {
			return nil, err
		}
		changed = append(changed, path)
	}
	return changed, nil
}

func writePreservingMode(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, info.Mode().Perm())
}

// Changelog is CHANGELOG.md at the root of the checkout.
const Changelog = "CHANGELOG.md"

var releaseHeading = regexp.MustCompile(`(?m)^## ([0-9]+\.[0-9]+\.[0-9]+) - ([0-9]{4}-[0-9]{2}-[0-9]{2})\r?$`)

// ReleaseNotes returns the CHANGELOG section of the version, without its
// heading, or an error when the changelog has no section for it.
func ReleaseNotes(root string, version Version) (string, error) {
	contents, err := os.ReadFile(filepath.Join(root, Changelog))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", Changelog, err)
	}
	text := strings.ReplaceAll(string(contents), "\r\n", "\n")
	for _, match := range releaseHeading.FindAllStringSubmatchIndex(text, -1) {
		if text[match[2]:match[3]] != version.String() {
			continue
		}
		body := text[match[1]:]
		if next := strings.Index(body, "\n## "); next >= 0 {
			body = body[:next]
		}
		body = strings.Trim(body, "\n")
		if body == "" {
			return "", fmt.Errorf("%s has an empty section for %s", Changelog, version)
		}
		return body + "\n", nil
	}
	return "", fmt.Errorf("%s has no section \"## %s - <date>\"", Changelog, version)
}

// StartRelease turns the Unreleased notes into the section of the version,
// dated today, and leaves an empty Unreleased section above it. A changelog
// that already has the version is left alone.
func StartRelease(root string, version Version, today time.Time) (bool, error) {
	path := filepath.Join(root, Changelog)
	contents, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", Changelog, err)
	}
	newline := "\n"
	if bytes.Contains(contents, []byte("\r\n")) {
		newline = "\r\n"
	}
	text := strings.ReplaceAll(string(contents), "\r\n", "\n")
	for _, match := range releaseHeading.FindAllStringSubmatch(text, -1) {
		if match[1] == version.String() {
			return false, nil
		}
	}
	const unreleased = "## Unreleased\n"
	index := strings.Index(text, unreleased)
	if index < 0 {
		return false, fmt.Errorf("%s has no \"## Unreleased\" section", Changelog)
	}
	pending := text[index+len(unreleased):]
	if next := strings.Index(pending, "\n## "); next >= 0 {
		pending = pending[:next]
	}
	if strings.TrimSpace(pending) == "" {
		return false, fmt.Errorf("%s has nothing under \"## Unreleased\" to release as %s", Changelog, version)
	}
	heading := fmt.Sprintf("## %s - %s\n", version, today.Format("2006-01-02"))
	text = text[:index] + unreleased + "\n" + heading + text[index+len(unreleased):]
	text = strings.ReplaceAll(text, heading+"\n\n", heading+"\n")
	return true, writePreservingMode(path, []byte(strings.ReplaceAll(text, "\n", newline)))
}
