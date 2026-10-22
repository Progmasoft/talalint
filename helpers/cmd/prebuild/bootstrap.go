// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Progmasoft/talalint/helpers/internal/console"
)

type bootstrapRunner interface {
	Run(name string, arguments ...string) error
	Output(name string, arguments ...string) (string, error)
	LookPath(name string) (string, error)
}

const (
	ghcupBootstrapCommit       = "69df703e3b1c701f42913740ca48f16e9026fe89"
	ghcupWindowsBootstrapHash  = "54A83F3A1E63879530A921351FFA6DE0F8E509DE51501E8E7EF0CBBD03AC7405"
	ghcupUnixBootstrapHash     = "C85ADD4CDCA779EA34BCBEEB3A311BBFED23FC96583311DF41FE4E73554EBA6D"
	maximumBootstrapScriptSize = 512 * 1024
)

// These immutable upstream artifacts replace the mutable haskell.org bootstrap
// endpoints. Keep the hashes alongside the source revision so a changed script
// cannot silently become code executed by a developer's machine.
var ghcupWindowsBootstrap = pinnedBootstrap{
	source: "https://raw.githubusercontent.com/haskell/ghcup-hs/" + ghcupBootstrapCommit + "/scripts/bootstrap/bootstrap-haskell.ps1",
	hash:   ghcupWindowsBootstrapHash,
}

var ghcupUnixBootstrap = pinnedBootstrap{
	source: "https://raw.githubusercontent.com/haskell/ghcup-hs/" + ghcupBootstrapCommit + "/scripts/bootstrap/bootstrap-haskell",
	hash:   ghcupUnixBootstrapHash,
}

type pinnedBootstrap struct {
	source string
	hash   string
}

func loadPinnedBootstrap(artifact pinnedBootstrap) ([]byte, error) {
	client := &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("refusing to follow a redirect for a pinned bootstrap script")
		},
	}
	return fetchPinnedBootstrap(context.Background(), client, artifact)
}

func fetchPinnedBootstrap(ctx context.Context, client *http.Client, artifact pinnedBootstrap) ([]byte, error) {
	parsed, err := url.ParseRequestURI(artifact.source)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "raw.githubusercontent.com" {
		return nil, errors.New("pinned bootstrap source must use HTTPS from raw.githubusercontent.com")
	}
	if client == nil {
		return nil, errors.New("pinned bootstrap HTTP client is nil")
	}
	expectedHash, err := hex.DecodeString(artifact.hash)
	if err != nil || len(expectedHash) != sha256.Size {
		return nil, errors.New("pinned bootstrap SHA-256 is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.source, nil)
	if err != nil {
		return nil, fmt.Errorf("create pinned bootstrap request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download pinned bootstrap script: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download pinned bootstrap script: HTTP %d", response.StatusCode)
	}
	if response.Request == nil || response.Request.URL.String() != artifact.source {
		return nil, errors.New("pinned bootstrap source changed during the request")
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumBootstrapScriptSize+1))
	if err != nil {
		return nil, fmt.Errorf("read pinned bootstrap script: %w", err)
	}
	if len(contents) > maximumBootstrapScriptSize {
		return nil, fmt.Errorf("pinned bootstrap script exceeds %d bytes", maximumBootstrapScriptSize)
	}
	actualHash := sha256.Sum256(contents)
	if !equalBytes(actualHash[:], expectedHash) {
		return nil, errors.New("pinned bootstrap script SHA-256 mismatch; refusing to execute it")
	}
	return contents, nil
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func withTemporaryBootstrapScript(contents []byte, pattern string, run func(string) error) error {
	script, err := os.CreateTemp("", pattern)
	if err != nil {
		return fmt.Errorf("create temporary bootstrap script: %w", err)
	}
	path := script.Name()
	defer os.Remove(path)
	if _, err := script.Write(contents); err != nil {
		_ = script.Close()
		return fmt.Errorf("write temporary bootstrap script: %w", err)
	}
	if err := script.Close(); err != nil {
		return fmt.Errorf("close temporary bootstrap script: %w", err)
	}
	return run(path)
}

// bootstrapSystemRunner installs tools. A package manager may ask for a
// password or an agreement, so its output is never held back.
type bootstrapSystemRunner struct {
	console *console.Console
}

func (runner bootstrapSystemRunner) Run(name string, arguments ...string) error {
	return runner.console.Live(console.Describe(name, arguments), func(stdout, stderr io.Writer) error {
		command := exec.Command(name, arguments...)
		command.Stdin = os.Stdin
		command.Stdout = stdout
		command.Stderr = stderr
		return command.Run()
	})
}

func (bootstrapSystemRunner) Output(name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (bootstrapSystemRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func installGHCupWindows(runner bootstrapRunner) error {
	return installGHCupWindowsWithLoader(runner, func() ([]byte, error) {
		return loadPinnedBootstrap(ghcupWindowsBootstrap)
	})
}

func installGHCupWindowsWithLoader(runner bootstrapRunner, load func() ([]byte, error)) error {
	script, err := load()
	if err != nil {
		return fmt.Errorf("cannot verify the pinned GHCup bootstrap: %w", err)
	}
	fmt.Println("Installing GHCup with the pinned Haskell bootstrap...")
	err = withTemporaryBootstrapScript(script, "ghcup-bootstrap-*.ps1", func(path string) error {
		return runner.Run("powershell.exe", "-NoProfile", "-NonInteractive", "-File", path,
			"-Minimal", "-InBash", "-InstallDir", `C:\`)
	})
	if err != nil {
		return fmt.Errorf("cannot install GHCup: %w", err)
	}
	return nil
}

func installGHCupUnix(runner bootstrapRunner) error {
	return installGHCupUnixWithLoader(runner, func() ([]byte, error) {
		return loadPinnedBootstrap(ghcupUnixBootstrap)
	})
}

func installGHCupUnixWithLoader(runner bootstrapRunner, load func() ([]byte, error)) error {
	return installGHCupUnixWithLoaderAs(runner, load, os.Geteuid() == 0)
}

func installGHCupUnixWithLoaderAs(runner bootstrapRunner, load func() ([]byte, error), runningAsRoot bool) error {
	if runningAsRoot {
		return errors.New("refusing to install per-user GHCup as root; rerun prebuild as the development user")
	}
	script, err := load()
	if err != nil {
		return fmt.Errorf("cannot verify the pinned GHCup bootstrap: %w", err)
	}
	fmt.Println("Installing GHCup with the pinned Haskell bootstrap...")
	err = withTemporaryBootstrapScript(script, "ghcup-bootstrap-*.sh", func(path string) error {
		return runner.Run("env", "BOOTSTRAP_HASKELL_NONINTERACTIVE=1", "BOOTSTRAP_HASKELL_MINIMAL=1", "sh", path)
	})
	if err != nil {
		return fmt.Errorf("cannot install GHCup: %w", err)
	}
	return nil
}

func findGHCup(runner bootstrapRunner) (string, error) {
	if path, err := runner.LookPath("ghcup"); err == nil {
		return path, nil
	}
	candidates := []string{`C:\ghcup\bin\ghcup.exe`}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".ghcup", "bin", "ghcup"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		candidates = append(candidates, filepath.Join(appData, "ghcup", "bin", "ghcup.exe"))
	}
	if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
		candidates = append(candidates, filepath.Join(userProfile, ".ghcup", "bin", "ghcup.exe"))
	}
	for _, candidate := range candidates {
		if information, err := os.Stat(candidate); err == nil && !information.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("ghcup was not found")
}
