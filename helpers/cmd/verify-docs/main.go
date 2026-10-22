// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

// verify-docs checks the documentation of the repository: every relative
// Markdown link must name a file that exists, and an anchor a heading of that
// file. With --haddock it also builds the Haddock reference and reports which
// exported entries lack a comment.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Progmasoft/talalint/helpers/internal/project"
	"github.com/spf13/cobra"
)

// skippedDirectories hold generated or downloaded Markdown.
var skippedDirectories = map[string]bool{
	".git": true, "node_modules": true, "dist-newstyle": true, "dist": true, "build": true,
	".gradle": true, ".kotlin": true, ".intellijPlatform": true, "out": true, ".codex": true, ".claude": true,
}

var (
	inlineLink    = regexp.MustCompile(`!?\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	referenceLink = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s+.*)?$`)
	heading       = regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+?)[ \t#]*$`)
	htmlAnchor    = regexp.MustCompile(`<a\s+(?:name|id)="([^"]+)"`)
	fence         = regexp.MustCompile("(?m)^\\s*(```|~~~)")
	inlineCode    = regexp.MustCompile("`[^`\n]*`")
)

// brokenLink is one link that names nothing.
type brokenLink struct {
	file, target, reason string
	line                 int
}

func (b brokenLink) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", b.file, b.line, b.target, b.reason)
}

// stripCode blanks fenced blocks, and inline code too when inline is set,
// keeping the line breaks so line numbers stay right: a link in an example is
// not a link, but a heading keeps its code for its anchor.
func stripCode(text string, inline bool) string {
	lines := strings.Split(text, "\n")
	inside := false
	for index, line := range lines {
		if fence.MatchString(line) {
			inside = !inside
			lines[index] = ""
			continue
		}
		if inside {
			lines[index] = ""
			continue
		}
		if inline {
			lines[index] = inlineCode.ReplaceAllStringFunc(line, func(code string) string { return strings.Repeat(" ", len(code)) })
		}
	}
	return strings.Join(lines, "\n")
}

// slug turns a heading into the anchor GitHub gives it.
func slug(text string) string {
	text = strings.ToLower(strings.TrimSpace(inlineCode.ReplaceAllStringFunc(text, func(code string) string { return strings.Trim(code, "`") })))
	var result strings.Builder
	for _, character := range text {
		switch {
		case character == ' ':
			result.WriteRune('-')
		case character == '-' || character == '_' || 'a' <= character && character <= 'z' || '0' <= character && character <= '9' || character > 127:
			result.WriteRune(character)
		}
	}
	return result.String()
}

// anchors lists the anchors of a Markdown file, numbering repeated headings
// the way GitHub does.
func anchors(text string) map[string]bool {
	found := map[string]bool{}
	seen := map[string]int{}
	for _, match := range heading.FindAllStringSubmatch(stripCode(text, false), -1) {
		base := slug(match[1])
		name := base
		if count := seen[base]; count > 0 {
			name = base + "-" + strconv.Itoa(count)
		}
		seen[base]++
		found[name] = true
	}
	for _, match := range htmlAnchor.FindAllStringSubmatch(text, -1) {
		found[match[1]] = true
	}
	return found
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

// checkLinks checks every relative link of every Markdown file below root.
func checkLinks(root string) ([]brokenLink, int, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skippedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(files)
	cache := map[string]map[string]bool{}
	anchorsOf := func(path string) (map[string]bool, error) {
		if found, ok := cache[path]; ok {
			return found, nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		cache[path] = anchors(strings.ReplaceAll(string(contents), "\r\n", "\n"))
		return cache[path], nil
	}
	var broken []brokenLink
	links := 0
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return nil, 0, err
		}
		text := stripCode(strings.ReplaceAll(string(contents), "\r\n", "\n"), true)
		relativeFile, _ := filepath.Rel(root, file)
		relativeFile = filepath.ToSlash(relativeFile)
		var matches [][]int
		matches = append(matches, inlineLink.FindAllStringSubmatchIndex(text, -1)...)
		matches = append(matches, referenceLink.FindAllStringSubmatchIndex(text, -1)...)
		for _, match := range matches {
			target := text[match[2]:match[3]]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			links++
			report := func(reason string) {
				broken = append(broken, brokenLink{relativeFile, target, reason, lineOf(text, match[0])})
			}
			path, anchor, _ := strings.Cut(target, "#")
			destination := file
			if path != "" {
				if strings.HasPrefix(path, "/") {
					report("absolute paths do not work outside GitHub; use a relative link")
					continue
				}
				destination = filepath.Join(filepath.Dir(file), filepath.FromSlash(path))
				info, err := os.Stat(destination)
				if err != nil {
					report("no such file")
					continue
				}
				if info.IsDir() {
					if anchor != "" {
						report("a directory has no anchors")
					}
					continue
				}
			}
			if anchor == "" || !strings.EqualFold(filepath.Ext(destination), ".md") {
				continue
			}
			found, err := anchorsOf(destination)
			if err != nil {
				return nil, 0, err
			}
			if !found[strings.ToLower(anchor)] {
				report("no heading has the anchor #" + anchor)
			}
		}
	}
	return broken, links, nil
}

var haddockCoverageLine = regexp.MustCompile(`(?m)^\s*(\d+)%\s+\(\s*(\d+)\s*/\s*(\d+)\)\s+in '([^']+)'`)

// moduleCoverage is Haddock's count for one module.
type moduleCoverage struct {
	module              string
	documented, entries int
}

func parseCoverage(output string) []moduleCoverage {
	var modules []moduleCoverage
	for _, match := range haddockCoverageLine.FindAllStringSubmatch(output, -1) {
		documented, _ := strconv.Atoi(match[2])
		entries, _ := strconv.Atoi(match[3])
		modules = append(modules, moduleCoverage{match[4], documented, entries})
	}
	return modules
}

// haddock builds the reference of the package and reports its coverage. It
// fails when Haddock fails, and with requireComplete also on any gap.
func haddock(root, packageName string, requireComplete bool, out io.Writer) error {
	command := exec.Command("cabal", "haddock", "lib:"+packageName, "--haddock-all")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		out.Write(output)
		return fmt.Errorf("cabal haddock failed: %w", err)
	}
	modules := parseCoverage(string(output))
	if len(modules) == 0 {
		return errors.New("Haddock printed no coverage report")
	}
	var gaps []string
	documented, entries := 0, 0
	for _, module := range modules {
		documented += module.documented
		entries += module.entries
		if module.documented < module.entries {
			gaps = append(gaps, fmt.Sprintf("%s %d/%d", module.module, module.documented, module.entries))
		}
	}
	fmt.Fprintf(out, "Haddock: %d of %d exported entries documented in %d modules.\n", documented, entries, len(modules))
	for _, gap := range gaps {
		fmt.Fprintf(out, "  incomplete: %s\n", gap)
	}
	if requireComplete && len(gaps) != 0 {
		return fmt.Errorf("%d modules have undocumented exports", len(gaps))
	}
	return nil
}

func newCommand(p project.Project, out, errOut io.Writer, getwd func() (string, error)) *cobra.Command {
	var withHaddock, requireComplete bool
	command := &cobra.Command{
		Use:   "verify-docs [--haddock [--require-complete]]",
		Short: "Check the Markdown links, and with --haddock the API reference.",
		Long: `Check the documentation of the repository.

Every relative Markdown link must name a file that exists, and its anchor a
heading of that file; links inside code are not links. With --haddock the
Haddock reference of the library is built and the exported entries without a
comment are listed; --require-complete makes them an error.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if requireComplete && !withHaddock {
				return errors.New("--require-complete needs --haddock")
			}
			directory, err := getwd()
			if err != nil {
				return err
			}
			root, err := p.FindRoot(directory)
			if err != nil {
				return err
			}
			broken, links, err := checkLinks(root)
			if err != nil {
				return err
			}
			for _, link := range broken {
				fmt.Fprintln(out, link)
			}
			var failures []error
			if len(broken) != 0 {
				failures = append(failures, fmt.Errorf("%d of %d relative links are broken", len(broken), links))
			} else {
				fmt.Fprintf(out, "Markdown: all %d relative links resolve.\n", links)
			}
			if withHaddock {
				if err := haddock(root, p.Name, requireComplete, out); err != nil {
					failures = append(failures, err)
				}
			}
			return errors.Join(failures...)
		},
	}
	command.Flags().BoolVar(&withHaddock, "haddock", false, "also build the Haddock reference and report its coverage")
	command.Flags().BoolVar(&requireComplete, "require-complete", false, "fail when an exported entry has no comment")
	command.SetOut(out)
	command.SetErr(errOut)
	return command
}

func main() {
	command := newCommand(project.Current, os.Stdout, os.Stderr, os.Getwd)
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
