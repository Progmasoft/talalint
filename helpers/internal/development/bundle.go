// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

package development

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Progmasoft/talalint/helpers/internal/project"
)

// bundleDocuments are copied next to the executable. LICENSES is a directory.
var bundleDocuments = []string{"LICENSE.txt", "LICENSES", "README.md", "CHANGELOG.md"}

// hostTarget names the host the way release archives do.
func hostTarget(goos, goarch string) string {
	architecture := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if architecture == "" {
		architecture = goarch
	}
	system := map[string]string{"darwin": "macos"}[goos]
	if system == "" {
		system = goos
	}
	return system + "-" + architecture
}

// bundleResult is what a bundle produced.
type bundleResult struct {
	Directory string
	Archive   string
	Checksum  string
}

// bundle builds the executable, stages it with the license and the notes,
// runs the staged copy, and packs it into an archive with its SHA-256.
func (w workspace) bundle(output string, report func(string, ...any)) (bundleResult, error) {
	if err := requireTools(w.runner, []project.Component{project.Haskell}); err != nil {
		return bundleResult{}, err
	}
	version, err := w.project.CurrentVersion(w.root)
	if err != nil {
		return bundleResult{}, err
	}
	if err := w.cabal("build", "exe:"+w.project.Name); err != nil {
		return bundleResult{}, err
	}
	listing, err := w.runner.Output(w.root, "cabal", "list-bin", "exe:"+w.project.Name)
	// Cabal may print what Git says about the pinned checkouts before the
	// path; the path is the last line.
	built := lastLine(listing)
	if err != nil || built == "" {
		return bundleResult{}, fmt.Errorf("cabal list-bin found no %s executable: %v", w.project.Name, err)
	}
	if output == "" {
		output = filepath.Join(w.root, "dist")
	}
	name := fmt.Sprintf("%s-%s-%s", w.project.Name, version, hostTarget(w.goos, runtime.GOARCH))
	directory := filepath.Join(output, name)
	// Only the folder and the files of this bundle are replaced; other
	// bundles in the output folder stay.
	if err := os.RemoveAll(directory); err != nil {
		return bundleResult{}, err
	}
	executable := filepath.Join(directory, "bin", executableName(w.goos, w.project.Name))
	if err := copyFile(built, executable, 0o755); err != nil {
		return bundleResult{}, err
	}
	for _, document := range bundleDocuments {
		if err := copyTree(filepath.Join(w.root, document), filepath.Join(directory, document)); err != nil {
			return bundleResult{}, err
		}
	}

	// The staged copy must run on its own and report the version it is
	// released as; a stale build or a wrong version file stops here.
	answer, err := w.runner.Output(directory, executable, "--version")
	want := w.project.Name + " " + version.String()
	if err != nil || strings.TrimSpace(answer) != want {
		return bundleResult{}, fmt.Errorf("the staged %s answered %q to --version, not %q (%v)", w.project.Name, answer, want, err)
	}
	report("the staged executable reports %s", want)

	stamp := w.sourceDate()
	archive := filepath.Join(output, name+".tar.gz")
	if w.goos == "windows" {
		archive = filepath.Join(output, name+".zip")
	}
	if err := writeArchive(archive, directory, name, stamp); err != nil {
		return bundleResult{}, err
	}
	sum, err := fileSHA256(archive)
	if err != nil {
		return bundleResult{}, err
	}
	checksum := archive + ".sha256"
	if err := os.WriteFile(checksum, []byte(sum+"  "+filepath.Base(archive)+"\n"), 0o644); err != nil {
		return bundleResult{}, err
	}
	return bundleResult{Directory: directory, Archive: archive, Checksum: sum}, nil
}

// sourceDate is the time every archive entry carries: SOURCE_DATE_EPOCH when
// it is set, otherwise the time of the commit, so the same commit packs into
// the same bytes.
func (w workspace) sourceDate() time.Time {
	if epoch := w.getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		if seconds, err := strconv.ParseInt(epoch, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}
	if output, err := w.runner.Output(w.root, "git", "log", "-1", "--format=%ct"); err == nil {
		if seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func copyFile(source, target string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

// copyTree copies a file, or a directory with everything below it.
func copyTree(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("the bundle needs %s: %w", filepath.Base(source), err)
	}
	if !info.IsDir() {
		return copyFile(source, target, 0o644)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(target, relative), 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		return copyFile(path, filepath.Join(target, relative), 0o644)
	})
}

// archiveEntry is one file of a bundle, with its path inside the archive.
type archiveEntry struct {
	path, name string
	executable bool
}

// collectEntries lists the files below directory in a fixed order, so an
// archive does not depend on the order the file system returns.
func collectEntries(directory, prefix string) ([]archiveEntry, error) {
	var entries []archiveEntry
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		entries = append(entries, archiveEntry{path: path, name: prefix + "/" + relative, executable: strings.HasPrefix(relative, "bin/")})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, err
}

// writeArchive packs directory under prefix into a .zip or a .tar.gz.
func writeArchive(archive, directory, prefix string, stamp time.Time) error {
	entries, err := collectEntries(directory, prefix)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("nothing to pack")
	}
	file, err := os.Create(archive)
	if err != nil {
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		err = writeZip(file, entries, stamp)
	} else {
		err = writeTarGz(file, entries, stamp)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(archive)
	}
	return err
}

func writeZip(output io.Writer, entries []archiveEntry, stamp time.Time) error {
	writer := zip.NewWriter(output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, Modified: stamp}
		mode := fs.FileMode(0o644)
		if entry.executable {
			mode = 0o755
		}
		header.SetMode(mode)
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if err := appendFile(target, entry.path); err != nil {
			return err
		}
	}
	return writer.Close()
}

func writeTarGz(output io.Writer, entries []archiveEntry, stamp time.Time) error {
	compressed, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return err
	}
	compressed.ModTime = stamp
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		info, err := os.Stat(entry.path)
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if entry.executable {
			mode = 0o755
		}
		header := &tar.Header{Name: entry.name, Mode: mode, Size: info.Size(), ModTime: stamp, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if err := appendFile(writer, entry.path); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

func appendFile(target io.Writer, path string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	_, err = io.Copy(target, input)
	return err
}

func fileSHA256(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
