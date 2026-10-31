// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

var sample = project.Project{
	Name:       "sample",
	Components: []project.Component{project.Haskell, project.Kotlin},
	Pins:       []project.Pin{{Name: "tala", Location: "https://github.com/Progmasoft/tala"}},
}

type fakeProbe struct {
	calls int
	fail  bool
}

func (p *fakeProbe) LookPath(name string) (string, error) {
	if name == "go" {
		return "/tools/go", nil
	}
	return "", errors.New("missing")
}

func (p *fakeProbe) Git(ctx context.Context, root string, args ...string) (string, error) {
	p.calls++
	if p.fail {
		return "", errors.New("secret stderr must never be included")
	}
	switch args[0] {
	case "branch":
		return "main", nil
	case "rev-parse":
		return "abcd", nil
	default:
		return " M tracked.go", nil
	}
}

func checkout(t *testing.T) string {
	root := t.TempDir()
	for path, text := range map[string]string{
		"sample.cabal":     "name: sample\nversion: 0.1.0\n",
		"build.gradle.kts": "version = \"0.1.0\"\n",
		"cabal.project":    "source-repository-package\n    type: git\n    location: https://github.com/Progmasoft/tala\n    tag: 45cc3c365e21818b257b8ef622ca35cb8283e766\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCollectReportsVersionPinsToolsAndChanges(t *testing.T) {
	p := &fakeProbe{}
	r := collect(context.Background(), sample, checkout(t), p)
	if r.Branch != "main" || r.Revision != "abcd" || !r.Dirty || p.calls != 3 {
		t.Fatalf("%+v", r)
	}
	if r.Version != "0.1.0" || len(r.Pins) != 1 || r.Pins[0].Commit != "45cc3c365e21818b257b8ef622ca35cb8283e766" {
		t.Fatalf("%+v", r)
	}
	if len(r.Tools) != 8 || !r.Tools[0].Available || r.Tools[1].Available {
		t.Fatal(r.Tools)
	}
}

func TestGitErrorsDoNotExposeSubprocessDetails(t *testing.T) {
	r := collect(context.Background(), sample, checkout(t), &fakeProbe{fail: true})
	if r.GitError == "" || strings.Contains(r.GitError, "secret") {
		t.Fatal(r)
	}
}

func TestJSONReportFromNestedDirectory(t *testing.T) {
	root := checkout(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--root", nested, "--json"}, &out, &out, sample, &fakeProbe{}); err != nil {
		t.Fatal(err)
	}
	var r report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Root != root || r.Revision != "abcd" || r.Project != "sample" {
		t.Fatalf("%+v", r)
	}
}

func TestTextReportNamesVersionProblems(t *testing.T) {
	root := checkout(t)
	if err := os.WriteFile(filepath.Join(root, "build.gradle.kts"), []byte("version = \"0.2.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--root", root}, &out, &out, sample, &fakeProbe{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Version: the version files disagree") {
		t.Fatal(out.String())
	}
}

func TestHelpAndInvalidArgumentsNeverProbe(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--unknown"}, {"unexpected"}} {
		p := &fakeProbe{}
		_ = run(args, &bytes.Buffer{}, &bytes.Buffer{}, sample, p)
		if p.calls != 0 {
			t.Fatal("Git executed")
		}
	}
}
