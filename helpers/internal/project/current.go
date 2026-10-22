// SPDX-FileCopyrightText: 2026 Progmasoft <support@progmasoft.com>
// SPDX-License-Identifier: MPL-2.0 WITH AdditionRef-Progmasoft-Exception-1.1

package project

// Current is the repository these helpers belong to. It is the only helper
// source that differs between the Tala tool repositories.
var Current = Project{
	Name:       "talalint",
	Components: []Component{Haskell, Kotlin},
	Pins: []Pin{
		{Name: "tala", Location: "https://github.com/Progmasoft/tala"},
		{Name: "talafmt", Location: "https://github.com/Progmasoft/talafmt"},
	},
}
