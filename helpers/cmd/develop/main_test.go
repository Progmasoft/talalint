// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// Exercise the exact public invocation, not just the implementation package.
func TestDeveloperEntrypointDelegatesHelp(t *testing.T) {
	output, err := exec.Command("go", "run", ".", "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("developer help failed: %v\n%s", err, output)
	}
	for _, expected := range []string{"Developer command of", "doctor", "check", "bundle", "coverage", "release-notes", "pin"} {
		if !strings.Contains(string(output), expected) {
			t.Errorf("developer help is missing %q", expected)
		}
	}
}

func TestDeveloperEntrypointRejectsAnUnknownCommand(t *testing.T) {
	output, err := exec.Command("go", "run", ".", "deploy").CombinedOutput()
	var failure *exec.ExitError
	if !errors.As(err, &failure) || !strings.Contains(string(output), "unknown command") {
		t.Fatalf("an unknown command was not refused: %v\n%s", err, output)
	}
}
