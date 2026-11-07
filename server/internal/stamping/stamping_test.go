// Package stamping_test checks that every query writing a row stamps it from the app's clock.
package stamping_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

var (
	queryName = regexp.MustCompile(`(?m)^-- name: (\w+)`)
	comments  = regexp.MustCompile(`--[^\n]*|/\*(?s:.*?)\*/`)
	space     = regexp.MustCompile(`\s+`)
	writes    = regexp.MustCompile(`\binsert into\b|\bupdate (?:only )?\S+ (?:(?:as )?\w+ )?set\b|\bmerge into\b`)
	insertOf  = regexp.MustCompile(`\binsert into ?([^\s(]*)(?: as \w+)?`)
	updateOf  = regexp.MustCompile(`\bupdate (?:only )?([^\s(]*)(?: (?:as )?\w+)? set\b`)
	upsert    = regexp.MustCompile(`\bdo update set\b`)
	mergeOf   = regexp.MustCompile(`\bmerge into ?([^\s(]*)`)
	branches  = regexp.MustCompile(`\bthen (insert|update set)\b`)
	setEnd    = regexp.MustCompile(`\b(?:where|returning|from)\b|$`)
	branchEnd = regexp.MustCompile(`\bwhen (?:not )?matched\b|$`)
	values    = regexp.MustCompile(`^ ?values ?\(`)
	named     = regexp.MustCompile(`^(?:"[^"]+"|[a-z_]\w*)(?:\.(?:"[^"]+"|[a-z_]\w*))?$`)
	dbClock   = regexp.MustCompile(`\b(?:now|clock_timestamp|statement_timestamp|transaction_timestamp) ?\(|\b(?:current_timestamp|localtimestamp)\b`)
)

// normalize drops comments and folds case and whitespace, so a write reads the same however it is laid out.
func normalize(sql string) string {
	return strings.ToLower(space.ReplaceAllString(comments.ReplaceAllString(sql, " "), " "))
}

// inParens returns what lies between the parenthesis at open and its match.
func inParens(s string, open int) (string, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return s[open+1 : i], true
			}
		}
	}
	return "", false
}

// items splits a list at its top-level commas.
func items(list string) []string {
	var out []string
	depth, from := 0, 0
	for i, c := range list {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(list[from:i]))
				from = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(list[from:]))
}

// insertProblems checks the column list and first values row that follow an insert into table.
func insertProblems(table, rest string) []string {
	rest = strings.TrimPrefix(rest, " ")
	var cols, vals []string
	if strings.HasPrefix(rest, "(") {
		if list, ok := inParens(rest, 0); ok {
			cols = items(list)
			rest = rest[len(list)+2:]
		}
	}
	if at := values.FindStringIndex(rest); at != nil {
		row, _ := inParens(rest, at[1]-1)
		vals = items(row)
	}
	var out []string
	for _, stamp := range []string{"created_at", "updated_at"} {
		i := slices.Index(cols, stamp)
		switch {
		case i < 0:
			out = append(out, fmt.Sprintf("an insert into %s leaves out %s", table, stamp))
		case i < len(vals) && dbClock.MatchString(vals[i]):
			out = append(out, fmt.Sprintf("an insert into %s stamps %s from the database's clock", table, stamp))
		}
	}
	return out
}

// setProblems checks the assignments of an update's set clause.
func setProblems(table, set string) []string {
	for _, a := range items(set) {
		col, val, _ := strings.Cut(a, "=")
		if strings.TrimSpace(col) != "updated_at" {
			continue
		}
		if dbClock.MatchString(val) {
			return []string{fmt.Sprintf("an update of %s stamps updated_at from the database's clock", table)}
		}
		return nil
	}
	return []string{fmt.Sprintf("an update of %s leaves updated_at", table)}
}

// clause is what follows at up to the first match of end.
func clause(s string, at int, end *regexp.Regexp) string {
	rest := s[at:]
	return rest[:end.FindStringIndex(rest)[0]]
}

const unnamed = "a write to a table it does not name"

// problems lists the writes in one query that leave a stamp out or take it from the database's clock.
func problems(sql string) []string {
	var out []string
	for statement := range strings.SplitSeq(normalize(sql), ";") {
		if m := mergeOf.FindStringSubmatchIndex(statement); m != nil {
			out = append(out, mergeProblems(statement, statement[m[2]:m[3]])...)
			continue
		}
		var updates []string
		for _, m := range insertOf.FindAllStringSubmatchIndex(statement, -1) {
			table := statement[m[2]:m[3]]
			if !named.MatchString(table) {
				return []string{unnamed}
			}
			out = append(out, insertProblems(table, statement[m[1]:])...)
		}
		for _, m := range updateOf.FindAllStringSubmatchIndex(statement, -1) {
			table := statement[m[2]:m[3]]
			if !named.MatchString(table) {
				return []string{unnamed}
			}
			updates = append(updates, setProblems(table, clause(statement, m[1], setEnd))...)
		}
		for _, m := range upsert.FindAllStringIndex(statement, -1) {
			updates = append(updates, setProblems("the conflicting row", clause(statement, m[1], setEnd))...)
		}
		out = append(out, updates...)
	}
	return out
}

// mergeProblems checks each branch of a merge into table, its inserts first.
func mergeProblems(statement, table string) []string {
	if !named.MatchString(table) {
		return []string{unnamed}
	}
	var inserts, updates []string
	for _, m := range branches.FindAllStringSubmatchIndex(statement, -1) {
		if statement[m[2]:m[3]] == "insert" {
			inserts = append(inserts, insertProblems(table, statement[m[1]:])...)
		} else {
			updates = append(updates, setProblems(table, clause(statement, m[1], branchEnd))...)
		}
	}
	return append(inserts, updates...)
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
		{"an update over lines", "UPDATE outbox\n\tSET published_at = $1\n\tWHERE event_id = $2", []string{"an update of outbox leaves updated_at"}},
		{"an update with double spaces", `UPDATE  t  SET  n = 1`, []string{"an update of t leaves updated_at"}},
		{"a comment inside a write", "UPDATE t /* the row */ SET n = 1 -- for now\n, updated_at = $1", nil},
		{"an aliased table", `UPDATE t AS x SET n = 1 WHERE x.id = $1`, []string{"an update of t leaves updated_at"}},
		{"an alias without as", `UPDATE t x SET n = 1, updated_at = $2 WHERE x.id = $1`, nil},
		{"a schema's table", `INSERT INTO public.t (id) VALUES ($1)`, []string{"an insert into public.t leaves out created_at", "an insert into public.t leaves out updated_at"}},
		{"a table filled in by fmt", `UPDATE %s SET n = 1`, []string{unnamed}},
		{"an insert into a table filled in by fmt", `INSERT INTO %s (id, created_at, updated_at) VALUES ($1, $2, $2)`, []string{unnamed}},
		{"an insert that ends at its table", `INSERT INTO `, []string{unnamed}},
		{"the database's clock in an update", `UPDATE t SET n = 1, updated_at = now() WHERE id = $1`, []string{"an update of t stamps updated_at from the database's clock"}},
		{"the database's clock in an insert", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, CURRENT_TIMESTAMP, $2)`, []string{"an insert into t stamps created_at from the database's clock"}},
		{"the database's clock elsewhere is fine", `UPDATE t SET n = 1, updated_at = $1 WHERE expires_at < now()`, nil},
		{"a stamped merge passes", `MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET n = s.n, updated_at = $1 WHEN NOT MATCHED THEN INSERT (id, created_at, updated_at) VALUES (s.id, $1, $1)`, nil},
		{"a merge stamps both its branches", `MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET n = s.n WHEN NOT MATCHED THEN INSERT (id, created_at) VALUES (s.id, $1)`,
			[]string{"an insert into t leaves out updated_at", "an update of t leaves updated_at"}},
		{"a merge into a table filled in by fmt", `MERGE INTO %s USING s ON true WHEN MATCHED THEN DELETE`, []string{unnamed}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := problems(tt.sql); !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The database fills no stamp, and an update that forgets updated_at keeps a stale time, so every query is checked.
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

type goWrite struct {
	at  token.Position
	sql string
}

// goWrites finds the SQL writes in the hand-written Go of the packages matching pattern, each constant read whole.
func goWrites(dir, pattern string) ([]goWrite, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo, Dir: dir}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, e)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	var found []goWrite
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			if ast.IsGenerated(f) {
				continue // buf's, and sqlc's from the query files above
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n.(type) {
				case *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr:
				default:
					return true
				}
				tv := p.TypesInfo.Types[n.(ast.Expr)]
				if tv.Value == nil || tv.Value.Kind() != constant.String {
					return true
				}
				if sql := constant.StringVal(tv.Value); writes.MatchString(normalize(sql)) {
					found = append(found, goWrite{p.Fset.Position(n.Pos()), sql})
				}
				return false
			})
		}
	}
	return found, nil
}

// The Go scan folds constants, so SQL split across concatenated literals and constants is read whole.
func TestTheGoScanReadsSQLBuiltFromConstants(t *testing.T) {
	found, err := goWrites(".", "./testdata")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		got = append(got, problems(f.sql)...)
	}
	want := []string{"an update of outbox leaves updated_at", "an insert into keys leaves out updated_at"}
	if !slices.Equal(got, want) {
		t.Fatalf("problems %q, want %q", got, want)
	}
}

// SQL written by hand in Go, such as the idempotency and outbox stores', is checked the same way.
func TestEveryHandWrittenWriteStampsItsRows(t *testing.T) {
	found, err := goWrites("../..", "./...")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("found no hand-written write to check")
	}
	for _, f := range found {
		for _, p := range problems(f.sql) {
			t.Errorf("%s: %s", f.at, p)
		}
	}
}
