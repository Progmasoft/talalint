<!-- SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com> -->
<!-- SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1 -->

# Developer helpers

The helpers are Go programs that build, test, pack and release talalint with the same commands CI runs. They need Go
1.26 or newer and work from any directory inside the checkout; `go.work` at the root lets them run from there:

```powershell
go run ./helpers/cmd/develop doctor
go run ./helpers/cmd/develop check
```

In a terminal every program they start is one step with a spinner; its whole output goes to a log file below the user
cache directory, and a failure prints the last lines and the path of that file. Without a terminal the output streams
as it is, and on GitHub Actions each step is a collapsible group. `TALALINT_HELPERS_OUTPUT=rich|plain|actions`
overrides the choice.

## Commands

| Command | What it does |
| --- | --- |
| `prebuild check` | Reports which host tools are missing. Changes nothing. |
| `prebuild install` | Installs the missing tools with winget, Homebrew, apt or dnf: Go, Git, GHCup (then GHC 9.10.3 and Cabal 3.16.1.0) and Temurin JDK 25. GHCup comes from its bootstrap script pinned by commit and SHA-256. |
| `develop doctor [--json]` | Finds every tool, reads its version and compares it with the versions CI uses. |
| `develop build [--component <name>]... [-- <Cabal options>]` | Builds every component, or the ones named. |
| `develop test [--component <name>]... [-- <Cabal options>]` | Builds and runs the tests. |
| `develop check [--component <name>]...` | Runs every gate of CI: the version files agree, the pins are commits, then the builds, tests and package checks. |
| `develop run [-- <talalint arguments>]` | Builds talalint and runs it. |
| `develop bundle [--output <directory>]` | Stages the executable with the license, README and changelog, runs it with `--version`, and packs a `.zip` (Windows) or `.tar.gz` with a `.sha256` file. The entries carry the commit time, so a commit packs into the same bytes. |
| `develop coverage [--component haskell\|kotlin]...` | Runs the tests with HPC and JaCoCo coverage and prints where the HTML reports are. |
| `develop format [--check]` | Formats the Kotlin sources with Spotless, or checks them. |
| `develop lint [-- <HLint options>]` | Runs HLint over the Haskell sources. HLint is optional: `cabal install hlint`. |
| `develop clean [--all] [--dry-run]` | Removes the build output (`dist-newstyle`, `dist`, the Gradle and npm build folders). `--all` also removes the downloaded caches. |
| `develop version` | Prints the version every version file states, and fails when they disagree. |
| `develop version <x.y.z>` | Checks that the checkout can be tagged `talalint-<x.y.z>`: every file states it, `CHANGELOG.md` has its notes, and the tag is new. |
| `develop version --set <x.y.z>` | Writes the version into every version file and turns the Unreleased notes into its changelog section. |
| `develop release-notes <x.y.z> [--output <file>]` | Prints the changelog section of a version, for the release page. |
| `repo-info [--json]` | Reports the checkout, version, pins and tools without changing anything. |
| `verify-docs [--haddock [--require-complete]]` | Checks every relative Markdown link and anchor; with `--haddock` also builds the Haddock reference and lists undocumented exports. |
| `verify-helpers` | Checks the helpers themselves: SPDX headers, gofmt, `go mod verify`, `go vet` and their tests. |
| `githelper start\|update\|sync\|push\|status\|clean` | The guarded Git workflow of the maintainers: it never commits on `main`, never force-pushes, and keeps generated output out of the index. |

## Components

`--component` selects among haskell, kotlin. The commands are those of the CI workflow:

| Component | `build` | `test` | `check` |
| --- | --- | --- | --- |
| `haskell` | `cabal build all` | `cabal test all` | `cabal check` |
| `kotlin` | `gradlew assemble` | `gradlew check` | `gradlew check jacocoTestReport` |

On Windows the helpers run Cabal with `core.longpaths` set through Git's environment configuration, in case a
repository is pinned again; nothing in the global Git configuration changes.

## Releasing

```powershell
go run ./helpers/cmd/develop version --set 0.2.0     # then review the changelog, commit and merge
go run ./helpers/cmd/develop version 0.2.0           # on main, before tagging
go run ./helpers/cmd/develop release-notes 0.2.0 --output notes.md
```

The release tag is `talalint-<x.y.z>` on the commit of `main` whose CI is green; the CI run of that commit has the
bundles of every host as artifacts.
