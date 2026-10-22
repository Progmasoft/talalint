// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package development

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

// showVersion prints the version every file states, or why they disagree.
func (w workspace) showVersion(out io.Writer) error {
	statements, err := w.project.ReadVersions(w.root)
	if err != nil {
		return err
	}
	version, err := w.project.CurrentVersion(w.root)
	for _, statement := range statements {
		fmt.Fprintf(out, "%-28s %s\n", statement.Path, statement.Version)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%s %s\n", w.project.Name, version)
	return nil
}

// checkRelease confirms that the checkout is ready to be tagged as version:
// every file states it, the changelog has its notes, and the tag is new.
func (w workspace) checkRelease(version project.Version, report func(string, ...any)) error {
	current, err := w.project.CurrentVersion(w.root)
	if err != nil {
		return err
	}
	if current != version {
		return fmt.Errorf("the version files state %s, not %s; run `develop version --set %s` first", current, version, version)
	}
	report("every version file states %s", version)
	if _, err := project.ReleaseNotes(w.root, version); err != nil {
		return err
	}
	report("%s has notes for %s", project.Changelog, version)
	tag := w.project.TagPrefix() + version.String()
	existing, err := w.runner.Output(w.root, "git", "tag", "--list", tag)
	if err != nil {
		return fmt.Errorf("git could not list the tags: %w", err)
	}
	// A tag made on GitHub is not in the checkout until it is fetched, so
	// origin is asked as well.
	remote, err := w.runner.Output(w.root, "git", "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return fmt.Errorf("git could not list the tags of origin: %w", err)
	}
	if strings.TrimSpace(existing) != "" || strings.TrimSpace(remote) != "" {
		return fmt.Errorf("the tag %s exists already; release tags are never moved", tag)
	}
	report("the tag %s is new", tag)
	return nil
}

// setVersion writes the version into every file and turns the Unreleased
// notes into its changelog section.
func (w workspace) setVersion(version project.Version, today time.Time, report func(string, ...any)) error {
	changed, err := w.project.SetVersion(w.root, version)
	if err != nil {
		return err
	}
	for _, path := range changed {
		report("%s now states %s", path, version)
	}
	started, err := project.StartRelease(w.root, version, today)
	if err != nil {
		return err
	}
	if started {
		report("%s has a section for %s with the unreleased notes", project.Changelog, version)
	}
	if len(changed) == 0 && !started {
		report("everything states %s already", version)
	}
	return nil
}

// showPins prints the commit cabal.project pins every repository to.
func (w workspace) showPins(out io.Writer) error {
	pins, err := w.project.ReadPins(w.root)
	if err != nil {
		return err
	}
	for _, pin := range pins {
		fmt.Fprintf(out, "%-10s %s  %s\n", pin.Pin.Name, pin.Commit, pin.Pin.Location)
	}
	return nil
}

// updatePin points a pin at a commit. A tag or a branch is resolved to its
// commit with git ls-remote, since cabal.project only keeps commits.
func (w workspace) updatePin(name, reference string, report func(string, ...any)) error {
	pin, err := w.project.FindPin(name)
	if err != nil {
		return err
	}
	commit := reference
	if !project.IsCommit(reference) {
		commit, err = w.resolveReference(pin, reference)
		if err != nil {
			return err
		}
		report("%s %s is %s", pin.Name, reference, commit)
	}
	changed, err := w.project.SetPin(w.root, pin, commit)
	if err != nil {
		return err
	}
	if !changed {
		report("%s is pinned to %s already", pin.Name, commit)
		return nil
	}
	report("%s now pins %s to %s; build and test before you commit it", project.CabalProject, pin.Name, commit)
	return nil
}

// resolveReference asks the remote for the commit of a tag or a branch. An
// annotated tag is answered twice; the peeled line names the commit.
func (w workspace) resolveReference(pin project.Pin, reference string) (string, error) {
	if strings.HasPrefix(reference, "-") || strings.ContainsAny(reference, " \t\r\n") {
		return "", fmt.Errorf("%q is not a Git reference", reference)
	}
	output, err := w.runner.Output(w.root, "git", "ls-remote", "--", pin.Location,
		"refs/tags/"+reference, "refs/tags/"+reference+"^{}", "refs/heads/"+reference)
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s failed: %w", pin.Location, err)
	}
	return pickReference(output, reference)
}

func pickReference(output, reference string) (string, error) {
	found := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		commit, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && project.IsCommit(commit) {
			found[name] = commit
		}
	}
	for _, name := range []string{"refs/tags/" + reference + "^{}", "refs/tags/" + reference, "refs/heads/" + reference} {
		if commit, ok := found[name]; ok {
			return commit, nil
		}
	}
	return "", fmt.Errorf("the remote has no tag or branch %q", reference)
}
