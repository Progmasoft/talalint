<!-- SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com> -->
<!-- SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1 -->

# talalint

talalint provides the `talalint` command. Its semantic diagnostics come directly from the canonical Tala compiler
frontend. The first independent checks cover trailing whitespace, mixed line endings, and a missing final newline.

```text
talalint Program.tala
talalint --fix Program.tala
talalint --help
talalint --version
```

talalint is the linter of [Tala](https://tala.run/). It moved from the
[compiler repository](https://github.com/Progmasoft/tala) with its history on 2026-10-18 and starts its own versions at
0.1.0.

The linter reports what the compiler it is built with reports. Version 0.1.0 is built with the compiler of
Tala 0.1.0: a source may use method references (`Type::Method`), names written through their namespace,
conditionals over strings and console output, and their diagnostics arrive under the `compiler` check, for example
`compiler.TAT0080` for a method reference that does not select one overload and `compiler.TAT0076` for a format the
compiler rejects.

`--fix` applies only safe physical-source fixes and never rewrites compiler or semantic diagnostics. `-h` and `-v` are
the short forms of `--help` and `--version`. The command line is read by a typed parser written by hand; a line it
cannot read exits with status 2.

The Kotlin module under `sources/main/kotlin` owns the typed `talalint.kts` configuration surface. It exposes the
complete rule catalog as typed scope properties and produces immutable snapshots without discovering or evaluating
scripts.

## Building and testing

talalint uses the compiler through the [`tala-tooling`](https://hackage.haskell.org/package/tala-tooling) package
from Hackage; the first build compiles it and the compiler packages it depends on. It does not use the formatter:
`--fix` applies the fixes of its own checks only.

```powershell
cabal update
cabal build all
cabal test all
.\gradlew.bat check      # the Kotlin configuration model: format check and tests
```

You need GHC 9.10 and Cabal 3.16, and JDK 25 for the Kotlin module.

## Developer helpers

The Go programs in [helpers](helpers/README.md) run the same commands as CI: `prebuild install` sets up a host,
`develop check` runs every gate, `develop bundle` packs a release archive for this host, and `develop version` and
`develop pin` keep the version files and the pinned commits in order.

```powershell
go run ./helpers/cmd/prebuild install
go run ./helpers/cmd/develop doctor
go run ./helpers/cmd/develop check
```
