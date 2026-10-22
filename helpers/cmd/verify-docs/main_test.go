// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

var sample = project.Project{Name: "sample", Components: []project.Component{project.Haskell}}

func write(t *testing.T, root, path, text string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checkout(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "cabal.project", "packages: sample.cabal\n")
	write(t, root, "sample.cabal", "name: sample\n")
	write(t, root, "docs/Guide.md", "# Guide\n\n## Command line\n\n## Command line\n\n## `--check` mode\n\n<a id=\"custom\"></a>\n")
	return root
}

func verify(t *testing.T, root string, arguments ...string) (string, error) {
	var out bytes.Buffer
	command := newCommand(sample, &out, &out, func() (string, error) { return filepath.Join(root, "docs"), nil })
	command.SetArgs(arguments)
	err := command.Execute()
	return out.String(), err
}

func TestGoodLinksPass(t *testing.T) {
	root := checkout(t)
	write(t, root, "README.md", strings.Join([]string{
		"[guide](docs/Guide.md) [section](docs/Guide.md#command-line) [second](docs/Guide.md#command-line-1)",
		"[mode](docs/Guide.md#--check-mode) [custom](docs/Guide.md#custom) [here](#readme) [folder](docs/)",
		"[web](https://example.com/missing) [mail](mailto:support@progmasoft.com)",
		"`[not a link](missing.md)`",
		"```",
		"[example](missing.md)",
		"```",
		"",
		"# Readme",
		"[ref]: docs/Guide.md",
	}, "\n"))
	output, err := verify(t, root)
	if err != nil || !strings.Contains(output, "all 8 relative links resolve") {
		t.Fatalf("%v\n%s", err, output)
	}
}

func TestBrokenLinksAreNamedWithTheirLine(t *testing.T) {
	root := checkout(t)
	write(t, root, "README.md", "intro\n[gone](docs/Missing.md)\n[anchor](docs/Guide.md#nothing)\n[root](/docs/Guide.md)\n[dir](docs/#x)\n")
	output, err := verify(t, root)
	if err == nil || !strings.Contains(err.Error(), "4 of 4 relative links are broken") {
		t.Fatalf("%v\n%s", err, output)
	}
	for _, want := range []string{"README.md:2: docs/Missing.md: no such file", "README.md:3: docs/Guide.md#nothing: no heading", "README.md:4: /docs/Guide.md: absolute paths", "README.md:5: docs/#x: a directory"} {
		if !strings.Contains(output, want) {
			t.Fatalf("%q lacks %q", output, want)
		}
	}
}

func TestGeneratedDirectoriesAreSkipped(t *testing.T) {
	root := checkout(t)
	write(t, root, "vscode/node_modules/pkg/README.md", "[gone](missing.md)\n")
	write(t, root, "dist-newstyle/src/x/README.md", "[gone](missing.md)\n")
	if output, err := verify(t, root); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}

func TestOptionsAreChecked(t *testing.T) {
	root := checkout(t)
	if _, err := verify(t, root, "--require-complete"); err == nil {
		t.Fatal("--require-complete without --haddock was accepted")
	}
	if _, err := verify(t, root, "extra"); err == nil {
		t.Fatal("an argument was accepted")
	}
}

func TestParseCoverage(t *testing.T) {
	output := " 100% ( 3 /  3) in 'Progmasoft.Talafmt'\n  50% ( 1 /  2) in 'Progmasoft.Talafmt.Report'\n"
	modules := parseCoverage(output)
	if len(modules) != 2 || modules[1].module != "Progmasoft.Talafmt.Report" || modules[1].documented != 1 || modules[1].entries != 2 {
		t.Fatalf("%+v", modules)
	}
}

func TestSlug(t *testing.T) {
	for heading, want := range map[string]string{"Command line": "command-line", "`--check` mode": "--check-mode", "Talafmt 0.1.0!": "talafmt-010", "Über": "über"} {
		if got := slug(heading); got != want {
			t.Errorf("slug(%q) = %q, want %q", heading, got, want)
		}
	}
}
