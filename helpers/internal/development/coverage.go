// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package development

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

// coverage runs the tests with coverage and returns the HTML reports: HPC for
// the Haskell package, JaCoCo for the Kotlin model.
func (w workspace) coverage(selected []project.Component) ([]string, error) {
	if err := requireTools(w.runner, selected); err != nil {
		return nil, err
	}
	var reports []string
	for _, component := range selected {
		switch component {
		case project.Haskell:
			if err := w.cabal("test", "all", "--enable-coverage", "--test-show-details=direct"); err != nil {
				return nil, err
			}
			reports = append(reports, findHPCReports(filepath.Join(w.root, "dist-newstyle"))...)
		case project.Kotlin:
			if err := w.gradle("test", "jacocoTestReport"); err != nil {
				return nil, err
			}
			reports = append(reports, filepath.Join(w.root, "build", "reports", "jacoco", "test", "html", "index.html"))
		}
	}
	return reports, nil
}

// findHPCReports lists the hpc_index.html of every test suite Cabal ran with
// coverage. Cabal keeps them below dist-newstyle/build/<platform>/<ghc>/.
func findHPCReports(root string) []string {
	var reports []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && (entry.Name() == "src" || entry.Name() == "cache" || entry.Name() == "packagedb") {
			return filepath.SkipDir
		}
		if entry.Name() == "hpc_index.html" && strings.Contains(filepath.ToSlash(path), "/hpc/") {
			reports = append(reports, path)
		}
		return nil
	})
	sort.Strings(reports)
	return reports
}
