// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package development

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

var sample = project.Project{
	Name:       "sample",
	Components: []project.Component{project.Haskell, project.Kotlin, project.IntelliJ, project.VSCode},
	Pins:       []project.Pin{{Name: "tala", Location: "https://github.com/Progmasoft/tala"}},
}

const talaCommit = "45cc3c365e21818b257b8ef622ca35cb8283e766"

// fakeRunner records what a command would start. Programs not in versions
// are missing; outputs answers Output calls by the program's base name.
type fakeRunner struct {
	versions map[string]string
	outputs  map[string]string
	fail     map[string]bool
	runs     []string
	// onRun lets a test act like the program, for example by writing a file.
	onRun func(name string, arguments []string)
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		versions: map[string]string{
			"git": "git version 2.51.0", "go": "go version go1.26.5 linux/amd64", "ghc": "9.10.3",
			"cabal": "3.16.1.0", "java": "openjdk 25.0.4 2026-07-21", "node": "v24.11.0", "npm": "11.6.2",
		},
		outputs: map[string]string{},
		fail:    map[string]bool{},
	}
}

func base(name string) string {
	return strings.TrimSuffix(strings.TrimSuffix(filepath.Base(name), ".exe"), ".bat")
}

func (f *fakeRunner) Run(directory string, environment []string, name string, arguments ...string) error {
	line := strings.TrimSpace(base(name) + " " + strings.Join(arguments, " "))
	f.runs = append(f.runs, line)
	if f.onRun != nil {
		f.onRun(name, arguments)
	}
	if f.fail[line] {
		return errors.New("exit status 1")
	}
	return nil
}

func (f *fakeRunner) Output(directory string, name string, arguments ...string) (string, error) {
	key := strings.TrimSpace(base(name) + " " + strings.Join(arguments, " "))
	if answer, ok := f.outputs[key]; ok {
		return answer, nil
	}
	if len(arguments) == 1 && strings.HasPrefix(arguments[0], "-") || len(arguments) == 1 && arguments[0] == "version" {
		if version, ok := f.versions[base(name)]; ok {
			return version, nil
		}
	}
	return "", fmt.Errorf("unexpected %s", key)
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if _, ok := f.versions[name]; ok {
		return "/tools/" + name, nil
	}
	return "", errors.New("not found")
}

func writeFile(t *testing.T, root, path, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sampleCheckout(t *testing.T) string {
	root := t.TempDir()
	writeFile(t, root, "sample.cabal", "name: sample\nversion: 0.1.0\n")
	writeFile(t, root, "build.gradle.kts", "version = \"0.1.0\"\n")
	writeFile(t, root, "intellij/build.gradle.kts", "version = \"0.1.0\"\n")
	writeFile(t, root, "vscode/package.json", "{\n  \"name\": \"sample\",\n  \"version\": \"0.1.0\",\n}\n")
	writeFile(t, root, "vscode/package-lock.json", "{\n  \"version\": \"0.1.0\",\n  \"packages\": {\n    \"\": {\n      \"version\": \"0.1.0\"\n    }\n  }\n}\n")
	writeFile(t, root, "cabal.project", "packages: sample.cabal\n\nsource-repository-package\n    type: git\n    location: https://github.com/Progmasoft/tala\n    tag: "+talaCommit+"\n")
	writeFile(t, root, "CHANGELOG.md", "# Changelog\n\n## Unreleased\n\n- Next.\n\n## 0.1.0 - 2026-10-18\n\nFirst.\n")
	writeFile(t, root, "LICENSE.txt", "license\n")
	writeFile(t, root, "LICENSES/Exception.txt", "exception\n")
	writeFile(t, root, "README.md", "# sample\n")
	return root
}

type harness struct {
	runner  *fakeRunner
	root    string
	out     bytes.Buffer
	reports []string
	opened  int
	goos    string
}

func newHarness(t *testing.T) *harness {
	return &harness{runner: newFakeRunner(), root: sampleCheckout(t), goos: "linux"}
}

func (h *harness) run(arguments ...string) error {
	h.out.Reset()
	env := environment{
		project: sample,
		runner:  h.runner,
		out:     &h.out,
		errOut:  &h.out,
		open: func() (workspace, error) {
			h.opened++
			return workspace{project: sample, root: h.root, runner: h.runner, goos: h.goos, getenv: func(string) string { return "" }}, nil
		},
		report: func(format string, arguments ...any) {
			h.reports = append(h.reports, fmt.Sprintf(format, arguments...))
		},
		table: func(header []string, rows [][]string) {
			for _, row := range append([][]string{header}, rows...) {
				fmt.Fprintln(&h.out, strings.Join(row, " | "))
			}
		},
		today: func() time.Time { return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC) },
	}
	return run(arguments, env)
}

func TestInvalidCommandLinesNeverOpenTheCheckout(t *testing.T) {
	for _, arguments := range [][]string{
		{"unknown"}, {"build", "extra"}, {"build", "--component", "rust"}, {"test", "x", "--", "y"},
		{"check", "x"}, {"run", "x"}, {"lint", "x"}, {"version", "1", "2"}, {"pin", "tala"},
		{"release-notes"}, {"clean", "now"}, {"coverage", "--component", "rust"}, {"bundle", "x"},
		{"coverage", "--component", "vscode"},
	} {
		h := newHarness(t)
		if err := h.run(arguments...); err == nil {
			t.Errorf("%v was accepted", arguments)
		}
		if h.opened != 0 || len(h.runner.runs) != 0 {
			t.Errorf("%v opened the checkout or ran %v", arguments, h.runner.runs)
		}
	}
	h := newHarness(t)
	if err := h.run("version", "1.2"); err == nil || len(h.runner.runs) != 0 {
		t.Fatal("a version without a patch number was accepted", err)
	}
}

func runtimeArch() string {
	return runtime.GOARCH
}

func TestBuildAndTestRunTheCommandsOfCI(t *testing.T) {
	h := newHarness(t)
	if err := h.run("test", "--", "--ghc-options=-O0"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cabal build all --ghc-options=-O0",
		"cabal test all --test-show-details=direct --ghc-options=-O0",
		"gradlew check",
		"gradlew -p intellij compileKotlin processResources",
		"npm ci",
		"npm run check",
	}
	if strings.Join(h.runner.runs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%q", h.runner.runs)
	}
	h.runner.runs = nil
	if err := h.run("build", "--component", "vscode", "--component", "haskell"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.runner.runs, "\n") != "npm ci\nnpm run compile\ncabal build all" {
		t.Fatalf("%q", h.runner.runs)
	}
}

func TestBuildStopsAtTheFirstFailure(t *testing.T) {
	h := newHarness(t)
	h.runner.fail["cabal build all"] = true
	err := h.run("build")
	if err == nil || !strings.HasPrefix(err.Error(), "haskell:") || len(h.runner.runs) != 1 {
		t.Fatal(err, h.runner.runs)
	}
}

func TestMissingToolsStopBeforeAnyBuild(t *testing.T) {
	h := newHarness(t)
	delete(h.runner.versions, "java")
	h.runner.versions["cabal"] = "3.10.3.0"
	err := h.run("build")
	if err == nil || !strings.Contains(err.Error(), "Java is missing") || !strings.Contains(err.Error(), "Cabal 3.10.3.0 is unsuitable") || len(h.runner.runs) != 0 {
		t.Fatal(err, h.runner.runs)
	}
	h.runner.runs = nil
	if err := h.run("build", "--component", "haskell"); err == nil {
		t.Fatal("an old Cabal was accepted")
	}
}

func TestCheckVerifiesMetadataFirst(t *testing.T) {
	h := newHarness(t)
	if err := h.run("check", "--component", "haskell", "--component", "kotlin"); err != nil {
		t.Fatal(err)
	}
	want := "cabal build all\ncabal test all --test-show-details=direct\ncabal check\ngradlew check jacocoTestReport"
	if strings.Join(h.runner.runs, "\n") != want {
		t.Fatalf("%q", h.runner.runs)
	}
	if h.reports[0] != "every version file states 0.1.0" || !strings.HasPrefix(h.reports[1], "tala is pinned to 45cc3c365e21") {
		t.Fatal(h.reports)
	}
	writeFile(t, h.root, "intellij/build.gradle.kts", "version = \"0.2.0\"\n")
	h.runner.runs = nil
	if err := h.run("check"); err == nil || len(h.runner.runs) != 0 {
		t.Fatal("disagreeing versions reached the build", err)
	}
}

func TestDoctor(t *testing.T) {
	h := newHarness(t)
	h.runner.versions["ghc"] = "9.12.2"
	if err := h.run("doctor"); err != nil {
		t.Fatal(err)
	}
	text := h.out.String()
	for _, want := range []string{"GHC | 9.12.2 | 9.10.3 | warning (CI builds with 9.10.3)", "HLint | - | any | optional, missing", "Node.js | 24.11.0", "can build every component"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q lacks %q", text, want)
		}
	}
	delete(h.runner.versions, "npm")
	if err := h.run("doctor", "--json"); err == nil || !strings.Contains(h.out.String(), "\"state\": \"missing\"") {
		t.Fatal(err, h.out.String())
	}
}

func TestVersionCommands(t *testing.T) {
	h := newHarness(t)
	if err := h.run("version"); err != nil || !strings.Contains(h.out.String(), "sample 0.1.0") {
		t.Fatal(err, h.out.String())
	}
	h.runner.outputs["git tag --list sample-0.1.0"] = "sample-0.1.0"
	h.runner.outputs["git ls-remote --tags origin refs/tags/sample-0.1.0"] = ""
	if err := h.run("version", "0.1.0"); err == nil || !strings.Contains(err.Error(), "exists already") {
		t.Fatal(err)
	}
	// A tag made on GitHub and not fetched yet.
	h.runner.outputs["git tag --list sample-0.1.0"] = ""
	h.runner.outputs["git ls-remote --tags origin refs/tags/sample-0.1.0"] = "1111111111111111111111111111111111111111\trefs/tags/sample-0.1.0"
	if err := h.run("version", "0.1.0"); err == nil || !strings.Contains(err.Error(), "exists already") {
		t.Fatal(err)
	}
	if err := h.run("version", "0.2.0"); err == nil || !strings.Contains(err.Error(), "--set 0.2.0") {
		t.Fatal(err)
	}
	if err := h.run("version", "0.2.0", "--set", "0.2.0"); err == nil {
		t.Fatal("a check and a set together were accepted")
	}
	if err := h.run("version", "--set", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	h.runner.outputs["git tag --list sample-0.2.0"] = ""
	h.runner.outputs["git ls-remote --tags origin refs/tags/sample-0.2.0"] = ""
	if err := h.run("version", "0.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("release-notes", "0.2.0"); err != nil || h.out.String() != "- Next.\n" {
		t.Fatalf("%q %v", h.out.String(), err)
	}
	changelog, _ := os.ReadFile(filepath.Join(h.root, "CHANGELOG.md"))
	if !strings.Contains(string(changelog), "## Unreleased\n\n## 0.2.0 - 2026-10-22\n") {
		t.Fatal(string(changelog))
	}
}

func TestPinResolvesTagsToCommits(t *testing.T) {
	h := newHarness(t)
	if err := h.run("pin"); err != nil || !strings.Contains(h.out.String(), "tala       "+talaCommit) {
		t.Fatal(err, h.out.String())
	}
	tagged := "1111111111111111111111111111111111111111"
	peeled := "2222222222222222222222222222222222222222"
	h.runner.outputs["git ls-remote -- https://github.com/Progmasoft/tala refs/tags/tala-0.2.0 refs/tags/tala-0.2.0^{} refs/heads/tala-0.2.0"] =
		tagged + "\trefs/tags/tala-0.2.0\n" + peeled + "\trefs/tags/tala-0.2.0^{}\n"
	if err := h.run("pin", "tala", "tala-0.2.0"); err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile(filepath.Join(h.root, "cabal.project"))
	if !strings.Contains(string(contents), "tag: "+peeled) {
		t.Fatal(string(contents))
	}
	if err := h.run("pin", "talafmt", peeled); !errors.Is(err, project.ErrNoPin) {
		t.Fatal(err)
	}
	if err := h.run("pin", "tala", "--upload-pack=x"); err == nil {
		t.Fatal("an option was passed to git as a reference")
	}
}

func TestPickReference(t *testing.T) {
	branch := "3333333333333333333333333333333333333333"
	if commit, err := pickReference(branch+"\trefs/heads/main\n", "main"); err != nil || commit != branch {
		t.Fatal(commit, err)
	}
	if _, err := pickReference("", "missing"); err == nil {
		t.Fatal("a missing reference was resolved")
	}
}

func TestRunAndLintForwardArguments(t *testing.T) {
	h := newHarness(t)
	if err := h.run("run", "--", "--check", "Main.tala"); err != nil {
		t.Fatal(err)
	}
	if h.runner.runs[0] != "cabal run --verbose=0 exe:sample -- --check Main.tala" {
		t.Fatal(h.runner.runs)
	}
	if err := h.run("lint"); err == nil || !strings.Contains(err.Error(), "cabal install hlint") {
		t.Fatal(err)
	}
	h.runner.versions["hlint"] = "3.10"
	writeFile(t, h.root, "sources/A.hs", "module A where\n")
	if err := h.run("lint", "--", "--json"); err != nil || h.runner.runs[len(h.runner.runs)-1] != "hlint sources --json" {
		t.Fatal(err, h.runner.runs)
	}
}

func TestFormatUsesSpotless(t *testing.T) {
	h := newHarness(t)
	if err := h.run("format", "--check"); err != nil || h.runner.runs[0] != "gradlew spotlessCheck" {
		t.Fatal(err, h.runner.runs)
	}
	h.goos = "windows"
	if err := h.run("format"); err != nil || h.runner.runs[1] != "gradlew spotlessApply" {
		t.Fatal(err, h.runner.runs)
	}
}

func TestCleanRemovesOnlyGeneratedOutput(t *testing.T) {
	h := newHarness(t)
	writeFile(t, h.root, "dist-newstyle/cache/plan.json", strings.Repeat("x", 2048))
	writeFile(t, h.root, "vscode/node_modules/a/index.js", "x")
	writeFile(t, h.root, "build", "a file named build is not output")
	if err := h.run("clean", "--dry-run"); err == nil {
		t.Fatal("a file named build was not reported")
	}
	if _, err := os.Stat(filepath.Join(h.root, "dist-newstyle")); err != nil {
		t.Fatal("a dry run removed output")
	}
	_ = h.run("clean")
	if _, err := os.Stat(filepath.Join(h.root, "dist-newstyle")); !os.IsNotExist(err) {
		t.Fatal("dist-newstyle stayed")
	}
	if _, err := os.Stat(filepath.Join(h.root, "vscode", "node_modules")); err != nil {
		t.Fatal("clean without --all removed node_modules")
	}
	if _, err := os.Stat(filepath.Join(h.root, "build")); err != nil {
		t.Fatal("a file was removed")
	}
	_ = h.run("clean", "--all")
	if _, err := os.Stat(filepath.Join(h.root, "vscode", "node_modules")); !os.IsNotExist(err) {
		t.Fatal("clean --all left node_modules")
	}
	if _, err := os.Stat(filepath.Join(h.root, "sample.cabal")); err != nil {
		t.Fatal("clean removed a source")
	}
}

func TestBundlePacksAReproducibleArchive(t *testing.T) {
	h := newHarness(t)
	built := filepath.Join(t.TempDir(), "sample")
	writeFile(t, filepath.Dir(built), "sample", "binary")
	h.runner.outputs["cabal list-bin exe:sample"] = "warning: git said something\n" + built
	h.runner.outputs["git log -1 --format=%ct"] = "1760745600"
	staged := filepath.Join(h.root, "dist", "sample-0.1.0-"+hostTarget("linux", runtimeArch())+"/bin/sample")
	h.runner.outputs[filepath.Base(staged)+" --version"] = "sample 0.1.0"
	if err := h.run("bundle"); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(h.root, "dist", "sample-0.1.0-"+hostTarget("linux", runtimeArch())+".tar.gz")
	first, err := fileSHA256(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum, _ := os.ReadFile(archive + ".sha256")
	if string(sum) != first+"  "+filepath.Base(archive)+"\n" {
		t.Fatal(string(sum))
	}
	if err := h.run("bundle"); err != nil {
		t.Fatal(err)
	}
	if second, _ := fileSHA256(archive); second != first {
		t.Fatal("the same commit packed into different bytes")
	}
	h.runner.outputs[filepath.Base(staged)+" --version"] = "sample 0.0.9"
	if err := h.run("bundle"); err == nil || !strings.Contains(err.Error(), "not \"sample 0.1.0\"") {
		t.Fatal(err)
	}
}

func TestZipBundleOnWindows(t *testing.T) {
	h := newHarness(t)
	h.goos = "windows"
	built := filepath.Join(t.TempDir(), "sample.exe")
	writeFile(t, filepath.Dir(built), "sample.exe", "binary")
	h.runner.outputs["cabal list-bin exe:sample"] = built
	h.runner.outputs["git log -1 --format=%ct"] = "1760745600"
	h.runner.outputs["sample --version"] = "sample 0.1.0"
	if err := h.run("bundle", "--output", filepath.Join(h.root, "out")); err != nil {
		t.Fatal(err)
	}
	name := "sample-0.1.0-" + hostTarget("windows", runtimeArch())
	if _, err := os.Stat(filepath.Join(h.root, "out", name+".zip")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "out", name, "bin", "sample.exe")); err != nil {
		t.Fatal(err)
	}
}

func TestCabalEnvironmentAddsLongPathsOnWindows(t *testing.T) {
	if got := cabalEnvironment("linux", func(string) string { return "" }); got != nil {
		t.Fatal(got)
	}
	got := cabalEnvironment("windows", func(name string) string {
		if name == "GIT_CONFIG_COUNT" {
			return "2"
		}
		return ""
	})
	want := []string{"GIT_CONFIG_COUNT=3", "GIT_CONFIG_KEY_2=core.longpaths", "GIT_CONFIG_VALUE_2=true"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatal(got)
	}
	merged := mergeEnvironment([]string{"A=1", "GIT_CONFIG_COUNT=2"}, want)
	if strings.Join(merged, " ") != "A=1 "+strings.Join(want, " ") {
		t.Fatal(merged)
	}
}

func TestHostTargetAndSizes(t *testing.T) {
	if hostTarget("darwin", "arm64") != "macos-aarch64" || hostTarget("windows", "amd64") != "windows-x86_64" || hostTarget("linux", "riscv64") != "linux-riscv64" {
		t.Fatal("unexpected target names")
	}
	if humanSize(512) != "512 B" || humanSize(3*1024*1024) != "3.0 MiB" {
		t.Fatal(humanSize(512), humanSize(3*1024*1024))
	}
	if compareVersions("3.16.1.0", "3.16") != 1 || compareVersions("3.16", "3.16.0") != 0 || compareVersions("9.8", "9.10") != -1 {
		t.Fatal("version order")
	}
}
