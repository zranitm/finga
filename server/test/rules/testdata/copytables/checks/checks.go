// Package checks gives notes rows, as a copy test's table does: Hello in its table, Bye only outside it.
package checks

import "finga/test/rules/testdata/copytables/notes"

func keepsEachNote() []any { return []any{notes.Hello{}, notes.Draft{}} }

func elsewhere() any { return &notes.Bye{} }

var _, _ = keepsEachNote, elsewhere
