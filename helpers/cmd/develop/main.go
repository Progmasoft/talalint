// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-2.0

// Command develop builds, tests, packs and releases the tool the way CI does.
package main

import "github.com/Progmasoft/talalint/helpers/internal/development"

func main() {
	development.Main()
}
