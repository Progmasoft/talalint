<!-- SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com> -->
<!-- SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1 -->

# Changelog

## Unreleased

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
