// Package stamping_test checks that every query writing a row stamps it from the app's clock.
package stamping_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	queryName = regexp.MustCompile(`(?m)^-- name: (\w+)`)
	comment   = regexp.MustCompile(`--[^\n]*`)
	space     = regexp.MustCompile(`\s+`)
	insert    = regexp.MustCompile(`\binsert into (\w+) ?(\(([^)]*)\))?`)
	update    = regexp.MustCompile(`\b(?:update (\w+) set|(do) update set)\b(.*?)(?:\bwhere\b|\breturning\b|\bfrom\b|;|$)`)
	setsStamp = regexp.MustCompile(`(?:^|,) ?updated_at ?=`)
)

// problems lists the writes in one query that leave a stamp out.
func problems(sql string) []string {
	sql = strings.ToLower(space.ReplaceAllString(comment.ReplaceAllString(sql, ""), " "))
	var out []string
	for _, m := range insert.FindAllStringSubmatch(sql, -1) {
		cols := strings.Split(strings.ReplaceAll(m[3], " ", ""), ",")
		for _, stamp := range []string{"created_at", "updated_at"} {
			if !slices.Contains(cols, stamp) {
				out = append(out, fmt.Sprintf("an insert into %s leaves out %s", m[1], stamp))
			}
		}
	}
	for _, m := range update.FindAllStringSubmatch(sql, -1) {
		if !setsStamp.MatchString(m[3]) {
			table := m[1]
			if m[2] != "" {
				table = "the conflicting row"
			}
			out = append(out, fmt.Sprintf("an update of %s leaves updated_at", table))
		}
	}
	return out
}

func TestProblems(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"a stamped insert passes", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, @now, @now);`, nil},
		{"an insert without a column list leaves both out", `INSERT INTO t VALUES ($1);`, []string{"an insert into t leaves out created_at", "an insert into t leaves out updated_at"}},
		{"an insert without updated_at", "INSERT INTO t (id,\n  created_at) VALUES ($1, $2);", []string{"an insert into t leaves out updated_at"}},
		{"a stamped update passes", `UPDATE t SET n = 1, updated_at = @now WHERE id = $1;`, nil},
		{"an update without updated_at", `UPDATE t SET n = 1 WHERE id = $1; -- updated_at = later`, []string{"an update of t leaves updated_at"}},
		{"a column ending in updated_at is not it", `UPDATE t SET last_updated_at = $2 WHERE id = $1;`, []string{"an update of t leaves updated_at"}},
		{"a lock is no update", `SELECT * FROM t WHERE id = $1 FOR UPDATE;`, nil},
		{"an upsert must stamp its update", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, $2, $2) ON CONFLICT (id) DO UPDATE SET n = 2;`, []string{"an update of the conflicting row leaves updated_at"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := problems(tt.sql); !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The database fills no stamp: an insert that names neither fails, but an update that forgets updated_at
// would keep a stale time, so every query is checked here.
func TestEveryWriteStampsItsRows(t *testing.T) {
	files, err := filepath.Glob("../../services/*/internal/repository/queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no query files (%v)", err)
	}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		names := queryName.FindAllStringSubmatchIndex(src, -1)
		for i, m := range names {
			end := len(src)
			if i+1 < len(names) {
				end = names[i+1][0]
			}
			for _, p := range problems(src[m[1]:end]) {
				t.Errorf("%s %s: %s", filepath.Base(file), src[m[2]:m[3]], p)
			}
		}
	}
}
