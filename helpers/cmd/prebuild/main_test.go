// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

type bootstrapFakeRunner struct {
	paths       map[string]string
	invocations [][]string
	installed   map[string]bool
	runHook     func(string, []string) error
}

func (runner *bootstrapFakeRunner) Run(name string, arguments ...string) error {
	runner.invocations = append(runner.invocations, append([]string{name}, arguments...))
	if runner.runHook != nil {
		return runner.runHook(name, arguments)
	}
	return nil
}

func (runner *bootstrapFakeRunner) Output(name string, arguments ...string) (string, error) {
	if name == "winget.exe" && len(arguments) >= 3 && arguments[0] == "list" {
		packageID := arguments[2]
		if runner.installed[packageID] {
			return "Name  Id  Version\n" + packageID + "  1.0", nil
		}
		return "No installed package found", errors.New("not installed")
	}
	if name == "brew" && len(arguments) == 3 && arguments[0] == "list" {
		formula := arguments[2]
		if runner.installed[formula] {
			return formula, nil
		}
		return "", errors.New("not installed")
	}
	return "", errors.New("not available")
}

func (runner *bootstrapFakeRunner) LookPath(name string) (string, error) {
	if path, ok := runner.paths[name]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func TestRequiredToolsFollowTheComponents(t *testing.T) {
	names := func(p project.Project) string {
		var result []string
		for _, requirement := range requiredTools(p) {
			result = append(result, requirement.name)
		}
		return strings.Join(result, ",")
	}
	haskellOnly := project.Project{Name: "a", Components: []project.Component{project.Haskell}}
	if got := names(haskellOnly); got != "Go,Git,GHCup" {
		t.Fatal(got)
	}
	everything := project.Project{Name: "b", Components: []project.Component{project.Haskell, project.Kotlin, project.IntelliJ, project.VSCode}}
	if got := names(everything); got != "Go,Git,GHCup,Temurin JDK 25,Node.js" {
		t.Fatal(got)
	}
}

func TestMissingToolsNeedsEveryExecutable(t *testing.T) {
	everything := project.Project{Name: "b", Components: []project.Component{project.Haskell, project.VSCode}}
	runner := &bootstrapFakeRunner{paths: map[string]string{"go": "go", "git": "git", "ghcup": "ghcup", "node": "node"}}
	missing := missingTools(everything, runner)
	if len(missing) != 1 || missing[0].name != "Node.js" {
		t.Fatalf("missing = %#v", missing)
	}
	runner.paths["npm"] = "npm"
	if missing := missingTools(everything, runner); len(missing) != 0 {
		t.Fatalf("a complete host still misses %#v", missing)
	}
}

func TestWingetInstallUsesExactNonInteractivePackageIdentity(t *testing.T) {
	runner := &bootstrapFakeRunner{}
	if err := installWingetPackage(runner, io.Discard, "winget.exe", "EclipseAdoptium.Temurin.25.JDK"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"winget.exe", "install", "--id", "EclipseAdoptium.Temurin.25.JDK", "--exact", "--source", "winget",
		"--accept-package-agreements", "--accept-source-agreements", "--silent", "--disable-interactivity",
	}
	if !reflect.DeepEqual(runner.invocations, [][]string{want}) {
		t.Fatalf("winget invocation = %#v", runner.invocations)
	}
	installed := &bootstrapFakeRunner{installed: map[string]bool{"Git.Git": true}}
	if err := installWingetPackage(installed, io.Discard, "winget.exe", "Git.Git"); err != nil || len(installed.invocations) != 0 {
		t.Fatalf("an installed package was installed again: %v %#v", err, installed.invocations)
	}
}

func TestHomebrewInstallsTheJDKAsACask(t *testing.T) {
	var jdk, node toolRequirement
	for _, requirement := range allTools {
		switch requirement.name {
		case "Temurin JDK 25":
			jdk = requirement
		case "Node.js":
			node = requirement
		}
	}
	if got := homebrewInstallArguments(jdk); !reflect.DeepEqual(got, []string{"install", "--cask", "temurin@25"}) {
		t.Fatalf("JDK arguments = %#v", got)
	}
	if got := homebrewInstallArguments(node); !reflect.DeepEqual(got, []string{"install", "node"}) {
		t.Fatalf("Node.js arguments = %#v", got)
	}
	runner := &bootstrapFakeRunner{installed: map[string]bool{"temurin@25": true}}
	if !homebrewPackageInstalled(runner, "brew", jdk) {
		t.Fatal("an installed cask was not recognized")
	}
}

func TestHaskellToolsArePinnedToTheVersionsOfCI(t *testing.T) {
	runner := &bootstrapFakeRunner{paths: map[string]string{"ghcup": "ghcup", "cabal": "cabal"}}
	if err := installHaskellTools(runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ghcup", "install", "ghc", "9.10.3", "--set"}}
	if !reflect.DeepEqual(runner.invocations, want) {
		t.Fatalf("GHCup invocations = %#v", runner.invocations)
	}
}

func TestReportNamesWhatIsMissing(t *testing.T) {
	haskell := project.Project{Name: "a", Components: []project.Component{project.Haskell}}
	runner := &bootstrapFakeRunner{paths: map[string]string{"go": "go", "git": "git", "ghcup": "ghcup", "ghc": "ghc"}}
	var output strings.Builder
	err := reportBootstrapState(haskell, bootstrapUbuntu, runner, &output)
	if err == nil || !strings.Contains(output.String(), "[missing] Cabal selected by GHCup") || !strings.Contains(output.String(), "[ready]   GHCup") {
		t.Fatalf("%v\n%s", err, output.String())
	}
}

func TestDetectBootstrapHostAcceptsWindowsAndMacOS(t *testing.T) {
	for goos, want := range map[string]bootstrapHost{"windows": bootstrapWindows, "darwin": bootstrapMacOS} {
		got, err := detectBootstrapHost(goos)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("host for %s = %v, want %v", goos, got, want)
		}
	}
}

func TestClassifyBootstrapLinuxUsesPinnedTierReleases(t *testing.T) {
	for _, test := range []struct {
		release string
		want    bootstrapHost
	}{
		{`ID=ubuntu` + "\n" + `VERSION_ID="26.04"`, bootstrapUbuntu},
		{`ID=debian` + "\n" + `VERSION_ID="13"`, bootstrapDebian},
		{`PRETTY_NAME="Debian GNU/Linux 13 (trixie)"` + "\n" + `ID=debian` + "\n" + `VERSION_ID="13"` + "\n" + `VERSION_CODENAME=trixie`, bootstrapDebian},
		{`ID=fedora` + "\n" + `VERSION_ID=43`, bootstrapFedora},
	} {
		got, err := classifyBootstrapLinux(test.release)
		if err != nil || got != test.want {
			t.Fatalf("classification of %q = %v, %v", test.release, got, err)
		}
	}
	if _, err := classifyBootstrapLinux("ID=fedora\nVERSION_ID=44"); err == nil {
		t.Fatal("Fedora N was accepted as the Fedora N-1 tier")
	}
}

func TestPinnedGHCupScriptsUseImmutableOfficialRevision(t *testing.T) {
	for name, artifact := range map[string]pinnedBootstrap{
		"Windows": ghcupWindowsBootstrap,
		"Unix":    ghcupUnixBootstrap,
	} {
		if !strings.HasPrefix(artifact.source, "https://raw.githubusercontent.com/haskell/ghcup-hs/"+ghcupBootstrapCommit+"/") {
			t.Errorf("%s source is not pinned to the reviewed GHCup revision: %s", name, artifact.source)
		}
		if decoded, err := hex.DecodeString(artifact.hash); err != nil || len(decoded) != sha256.Size {
			t.Errorf("%s script hash is invalid: %v", name, err)
		}
	}
}

type bootstrapRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper bootstrapRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func TestFetchPinnedBootstrapRejectsMismatchedAndOversizedContent(t *testing.T) {
	contents := []byte("Write-Output 'verified bootstrap'\n")
	digest := sha256.Sum256(contents)
	artifact := pinnedBootstrap{
		source: "https://raw.githubusercontent.com/haskell/ghcup-hs/reviewed/bootstrap.ps1",
		hash:   hex.EncodeToString(digest[:]),
	}
	clientFor := func(body string, status int) *http.Client {
		return &http.Client{Transport: bootstrapRoundTripper(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		})}
	}

	got, err := fetchPinnedBootstrap(context.Background(), clientFor(string(contents), http.StatusOK), artifact)
	if err != nil || string(got) != string(contents) {
		t.Fatalf("valid pinned content = %q, %v", got, err)
	}
	if _, err := fetchPinnedBootstrap(context.Background(), clientFor("untrusted content", http.StatusOK), artifact); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("mismatched script was not rejected: %v", err)
	}
	if _, err := fetchPinnedBootstrap(context.Background(), clientFor("not found", http.StatusNotFound), artifact); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("unexpected HTTP status was not rejected: %v", err)
	}
	oversized := strings.Repeat("x", maximumBootstrapScriptSize+1)
	if _, err := fetchPinnedBootstrap(context.Background(), clientFor(oversized, http.StatusOK), artifact); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized script was not rejected: %v", err)
	}
	if _, err := fetchPinnedBootstrap(context.Background(), clientFor(string(contents), http.StatusOK), pinnedBootstrap{
		source: "http://raw.githubusercontent.com/haskell/ghcup-hs/reviewed/bootstrap.ps1",
		hash:   artifact.hash,
	}); err == nil {
		t.Fatal("non-HTTPS script source was accepted")
	}
	redirectedClient := &http.Client{Transport: bootstrapRoundTripper(func(request *http.Request) (*http.Response, error) {
		redirectedRequest := request.Clone(request.Context())
		redirectedRequest.URL = request.URL.ResolveReference(&url.URL{Path: "/unreviewed/bootstrap.ps1"})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(contents))),
			Header:     make(http.Header),
			Request:    redirectedRequest,
		}, nil
	})}
	if _, err := fetchPinnedBootstrap(context.Background(), redirectedClient, artifact); err == nil || !strings.Contains(err.Error(), "changed during the request") {
		t.Fatalf("redirected source was not rejected: %v", err)
	}
}

func TestWindowsGHCupExecutesVerifiedLocalScriptWithoutCommandInterpolation(t *testing.T) {
	trustedScript := []byte("Write-Output 'known bootstrap bytes'\n")
	loaded := false
	runner := &bootstrapFakeRunner{
		runHook: func(name string, arguments []string) error {
			if name != "powershell.exe" || len(arguments) != 8 || arguments[0] != "-NoProfile" ||
				arguments[1] != "-NonInteractive" || arguments[2] != "-File" || arguments[4] != "-Minimal" ||
				arguments[5] != "-InBash" || arguments[6] != "-InstallDir" || arguments[7] != `C:\` {
				return errors.New("bootstrap was not passed as a local PowerShell file")
			}
			actual, err := os.ReadFile(arguments[3])
			if err != nil || string(actual) != string(trustedScript) {
				return errors.New("temporary bootstrap content did not match verified bytes")
			}
			return nil
		},
	}
	if err := installGHCupWindowsWithLoader(runner, func() ([]byte, error) {
		loaded = true
		return trustedScript, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !loaded || len(runner.invocations) != 1 {
		t.Fatalf("verified loader or GHCup invocation missing: loaded=%v invocations=%#v", loaded, runner.invocations)
	}
	path := runner.invocations[0][4]
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary bootstrap script was not removed after execution: %v", err)
	}
	if strings.Contains(strings.Join(runner.invocations[0], " "), "-Command") {
		t.Fatal("verified bootstrap was interpolated into a PowerShell command string")
	}
}

func TestBootstrapInstallersDoNotRunWhenVerificationFails(t *testing.T) {
	runner := &bootstrapFakeRunner{}
	loadFailure := func() ([]byte, error) { return nil, errors.New("pinned hash mismatch") }
	if err := installGHCupWindowsWithLoader(runner, loadFailure); err == nil {
		t.Fatal("Windows bootstrap accepted a verification failure")
	}
	if err := installGHCupUnixWithLoaderAs(runner, loadFailure, false); err == nil {
		t.Fatal("Unix bootstrap accepted a verification failure")
	}
	if len(runner.invocations) != 0 {
		t.Fatalf("installer ran before verification succeeded: %#v", runner.invocations)
	}
}

func TestUnixGHCupRefusesRootAndDoesNotFetchBootstrap(t *testing.T) {
	runner := &bootstrapFakeRunner{}
	loaderCalled := false
	err := installGHCupUnixWithLoaderAs(runner, func() ([]byte, error) {
		loaderCalled = true
		return []byte("must not run"), nil
	}, true)
	if err == nil || !strings.Contains(err.Error(), "as root") {
		t.Fatalf("root GHCup install was not rejected: %v", err)
	}
	if loaderCalled || len(runner.invocations) != 0 {
		t.Fatalf("root refusal fetched or executed code: fetched=%v runs=%#v", loaderCalled, runner.invocations)
	}
}

func TestUnixGHCupExecutesVerifiedFileWithoutShellCommandString(t *testing.T) {
	trustedScript := []byte("printf '%s\\n' 'known bootstrap bytes'\n")
	runner := &bootstrapFakeRunner{
		runHook: func(name string, arguments []string) error {
			if name != "env" || len(arguments) != 4 || arguments[0] != "BOOTSTRAP_HASKELL_NONINTERACTIVE=1" ||
				arguments[1] != "BOOTSTRAP_HASKELL_MINIMAL=1" || arguments[2] != "sh" {
				return errors.New("bootstrap was launched through a shell command string")
			}
			actual, err := os.ReadFile(arguments[3])
			if err != nil || string(actual) != string(trustedScript) {
				return errors.New("temporary bootstrap content did not match verified bytes")
			}
			return nil
		},
	}
	if err := installGHCupUnixWithLoaderAs(runner, func() ([]byte, error) { return trustedScript, nil }, false); err != nil {
		t.Fatal(err)
	}
	path := runner.invocations[0][4]
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary bootstrap script was not removed after execution: %v", err)
	}
}
