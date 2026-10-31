// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CabalProject is the Cabal project file at the root of the checkout.
const CabalProject = "cabal.project"

// commitPattern is a full Git commit name. Cabal accepts a branch or a tag as
// well, but only a commit keeps a build reproducible.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// IsCommit reports whether text is a full lowercase commit name.
func IsCommit(text string) bool {
	return commitPattern.MatchString(text)
}

// PinnedCommit is what cabal.project says about one pin.
type PinnedCommit struct {
	Pin    Pin
	Commit string
}

// blockPattern finds a source-repository-package block and its location and
// tag lines; the other fields of the block are not read.
var blockPattern = regexp.MustCompile(`(?m)^source-repository-package[ \t]*\r?\n((?:[ \t]+.*\r?\n?)+)`)
var fieldPattern = regexp.MustCompile(`(?m)^[ \t]+(location|tag):[ \t]*(\S+)[ \t]*\r?$`)

type block struct {
	location, tag string
	// tagStart and tagEnd delimit the tag value in the file.
	tagStart, tagEnd int
}

func readBlocks(contents string) []block {
	var blocks []block
	for _, match := range blockPattern.FindAllStringSubmatchIndex(contents, -1) {
		body := contents[match[2]:match[3]]
		var current block
		for _, field := range fieldPattern.FindAllStringSubmatchIndex(body, -1) {
			value := body[field[4]:field[5]]
			switch body[field[2]:field[3]] {
			case "location":
				current.location = value
			case "tag":
				current.tag = value
				current.tagStart, current.tagEnd = match[2]+field[4], match[2]+field[5]
			}
		}
		blocks = append(blocks, current)
	}
	return blocks
}

func sameLocation(a, b string) bool {
	normalize := func(url string) string {
		return strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(url), "/"), ".git")
	}
	return normalize(a) == normalize(b)
}

// ReadPins returns the commit cabal.project pins for every pin of the project.
// A pin without a block, or with a tag that is not a full commit, is an error.
func (p Project) ReadPins(root string) ([]PinnedCommit, error) {
	contents, err := os.ReadFile(filepath.Join(root, CabalProject))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", CabalProject, err)
	}
	blocks := readBlocks(string(contents))
	var pinned []PinnedCommit
	for _, pin := range p.Pins {
		found := false
		for _, candidate := range blocks {
			if !sameLocation(candidate.location, pin.Location) {
				continue
			}
			if !IsCommit(candidate.tag) {
				return nil, fmt.Errorf("%s pins %s to %q, which is not a full commit", CabalProject, pin.Name, candidate.tag)
			}
			pinned = append(pinned, PinnedCommit{pin, candidate.tag})
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("%s has no source-repository-package for %s", CabalProject, pin.Location)
		}
	}
	return pinned, nil
}

// SetPin points the block of the pin at commit. It returns false when the
// block already names that commit.
func (p Project) SetPin(root string, pin Pin, commit string) (bool, error) {
	if !IsCommit(commit) {
		return false, fmt.Errorf("%q is not a full commit", commit)
	}
	path := filepath.Join(root, CabalProject)
	contents, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", CabalProject, err)
	}
	text := string(contents)
	for _, candidate := range readBlocks(text) {
		if !sameLocation(candidate.location, pin.Location) {
			continue
		}
		if candidate.tagEnd == 0 {
			return false, fmt.Errorf("the %s block of %s has no tag", pin.Name, CabalProject)
		}
		if candidate.tag == commit {
			return false, nil
		}
		text = text[:candidate.tagStart] + commit + text[candidate.tagEnd:]
		return true, writePreservingMode(path, []byte(text))
	}
	return false, fmt.Errorf("%s has no source-repository-package for %s", CabalProject, pin.Location)
}
