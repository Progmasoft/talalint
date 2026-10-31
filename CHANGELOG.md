<!-- SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com> -->
<!-- SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0 -->

# Changelog

## Unreleased

- Project files move to the Progmasoft Linking Exception, Version 2.0. Earlier
  releases can rely on Version 2.0 too.

## 0.2.0 - 2026-10-25

- The compiler packages, `tala-tooling` and the packages it depends on, come
  from Hackage instead of a pinned commit of Progmasoft/tala.
- talalint no longer depends on talafmt. `--fix` applies the fixes of the
  source-hygiene checks only: it removes trailing spaces and tabs, ends every
  line the way the first one ends, and adds a final line break; it no longer
  reformats the file. `applySafeFixes` now takes and returns the source text.
- Sources are read and written as UTF-8 without line-ending translation, so
  on Windows the line-ending check sees CRLF as written.

## 0.1.0 - 2026-10-18

The first release of talalint from a repository of its own. Until now talalint
lived in the Tala repository, last as version 0.2.0; it moved with its history
and starts its own numbering.

- talalint reports the diagnostics of the compiler of Tala 0.1.0 under the
  `compiler` check, through `tala-tooling`, pinned to the `tala-0.1.0` commit of
  Progmasoft/tala until the compiler packages are on Hackage.
- Its own checks cover trailing whitespace, mixed line endings and a missing
  final newline; `--fix` repairs them with the talafmt library, taken from
  Progmasoft/talafmt.
- `talalint.kts` configures it through a typed Kotlin model.
