// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package development

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

// generatedOutput lists what the builds write into the checkout. Nothing else
// is removed: sources, local configuration and untracked work stay.
func generatedOutput(p project.Project, everything bool) []string {
	paths := []string{"dist-newstyle", "dist"}
	if p.Has(project.Kotlin) {
		paths = append(paths, "build")
	}
	if p.Has(project.IntelliJ) {
		paths = append(paths, "intellij/build")
	}
	if p.Has(project.VSCode) {
		paths = append(paths, "vscode/out")
	}
	if !everything {
		return paths
	}
	// Caches that a build downloads again: slow to rebuild, but generated.
	if p.Has(project.Kotlin) {
		paths = append(paths, ".gradle", ".kotlin")
	}
	if p.Has(project.IntelliJ) {
		paths = append(paths, "intellij/.gradle", "intellij/.kotlin", "intellij/.intellijPlatform")
	}
	if p.Has(project.VSCode) {
		paths = append(paths, "vscode/node_modules")
	}
	return paths
}

// directorySize adds up the bytes of the regular files below path.
func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, suffix := float64(bytes), ""
	for _, next := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		suffix = next
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}

// clean removes the generated output of the checkout and reports what it
// freed. With dryRun it only reports.
func (w workspace) clean(everything, dryRun bool, report func(string, ...any)) error {
	var freed int64
	var failures []string
	for _, relative := range generatedOutput(w.project, everything) {
		path := filepath.Join(w.root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", relative, err))
			continue
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			failures = append(failures, relative+" is not a directory; left in place")
			continue
		}
		size := directorySize(path)
		if dryRun {
			report("would remove %s (%s)", relative, humanSize(size))
			freed += size
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			// A running Gradle daemon holds files below .gradle on Windows.
			failures = append(failures, fmt.Sprintf("%s: %v", relative, err))
			continue
		}
		report("removed %s (%s)", relative, humanSize(size))
		freed += size
	}
	verb := "freed"
	if dryRun {
		verb = "would free"
	}
	report("%s %s", verb, humanSize(freed))
	if len(failures) != 0 {
		return fmt.Errorf("some output could not be removed: %s", strings.Join(failures, "; "))
	}
	return nil
}
