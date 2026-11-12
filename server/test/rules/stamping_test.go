// Every query writing a row stamps it from the app's clock.

package rules_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	pg "github.com/pganalyze/pg_query_go/v6"
	"golang.org/x/tools/go/packages"
	"google.golang.org/protobuf/reflect/protopath"
	"google.golang.org/protobuf/reflect/protorange"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var queryName = regexp.MustCompile(`(?m)^-- name: (\w+)`)

var stamps = []string{"created_at", "updated_at"}

// clockFuncs are the database's clock functions, matched by their last name so pg_catalog.now() counts too.
var clockFuncs = []string{"now", "clock_timestamp", "statement_timestamp", "transaction_timestamp", "timeofday"}

// clockWords are the inputs a timestamp reads from the database's clock, as in 'now'::timestamptz.
var clockWords = []string{"now", "today", "tomorrow", "yesterday"}

const (
	unparsed = "a write it cannot parse"
	runTime  = "a write put together at run time, which it cannot read"
	starred  = "a * it cannot read"
)

// each calls visit on every message in m's tree, m first, in field order.
func each(m protoreflect.ProtoMessage, visit func(protoreflect.ProtoMessage)) {
	_ = protorange.Options{Stable: true}.Range(m.ProtoReflect(), func(p protopath.Values) error {
		if v, ok := p.Index(-1).Value.Interface().(protoreflect.Message); ok {
			visit(v.Interface())
		}
		return nil
	}, nil)
}

// isWrite reports whether sql holds an insert, update or merge, read by Postgres's scanner so comments and quoted text don't count.
func isWrite(sql string) bool {
	scan, err := pg.Scan(sql)
	if err != nil {
		return false
	}
	var prev pg.Token
	updating := false
	for _, t := range scan.Tokens {
		switch t.Token {
		case pg.Token_SQL_COMMENT, pg.Token_C_COMMENT:
			continue
		case pg.Token_INTO:
			if prev == pg.Token_INSERT || prev == pg.Token_MERGE {
				return true
			}
		case pg.Token_UPDATE:
			updating = true
		case pg.Token_SET:
			if updating {
				return true
			}
		case pg.Token_ASCII_59:
			updating = false
		}
		prev = t.Token
	}
	return false
}

// source names where v takes a stamp from when that is the database, or is empty when the app gives it.
func source(v *pg.Node) string {
	if v.GetSetToDefault() != nil {
		return "the database's default"
	}
	clock := false
	each(v, func(m protoreflect.ProtoMessage) {
		switch m := m.(type) {
		case *pg.FuncCall:
			clock = clock || slices.Contains(clockFuncs, m.Funcname[len(m.Funcname)-1].GetString_().GetSval())
		case *pg.SQLValueFunction:
			clock = clock || m.Op <= pg.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP_N // the date and time ones come first
		case *pg.A_Const:
			clock = clock || slices.Contains(clockWords, strings.ToLower(strings.TrimSpace(m.GetSval().GetSval())))
		}
	})
	if clock {
		return "the database's clock"
	}
	return ""
}

// rows lists the rows a select gives an insert, the arms of a union each in turn.
func rows(s *pg.SelectStmt) [][]*pg.Node {
	switch {
	case s == nil:
		return nil
	case s.Op != pg.SetOperation_SETOP_NONE:
		return append(rows(s.Larg), rows(s.Rarg)...)
	case len(s.ValuesLists) > 0:
		var out [][]*pg.Node
		for _, l := range s.ValuesLists {
			out = append(out, l.GetList().GetItems())
		}
		return out
	}
	var row []*pg.Node
	for _, t := range s.TargetList {
		row = append(row, t.GetResTarget().GetVal())
	}
	return [][]*pg.Node{row}
}

// fills is where row's value for column i comes from when that is not the app; a * before it hides which value that is.
func fills(row []*pg.Node, i int) string {
	for _, v := range row[:min(i+1, len(row))] {
		if slices.ContainsFunc(v.GetColumnRef().GetFields(), func(f *pg.Node) bool { return f.GetAStar() != nil }) {
			return starred
		}
	}
	if i < len(row) {
		return source(row[i])
	}
	return ""
}

// insertProblems checks that an insert names both stamps and fills them from the app in every row.
func insertProblems(write string, cols []*pg.Node, rows [][]*pg.Node) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.GetResTarget().GetName()
	}
	var out []string
	for _, stamp := range stamps {
		i := slices.Index(names, stamp)
		if i < 0 {
			out = append(out, fmt.Sprintf("%s leaves out %s", write, stamp))
			continue
		}
		for _, row := range rows {
			if from := fills(row, i); from != "" {
				out = append(out, fmt.Sprintf("%s stamps %s from %s", write, stamp, from))
				break
			}
		}
	}
	return out
}

// assigned is what a (a, b) = (x, y) assignment gives one column: its item of the row, or the whole subquery.
func assigned(m *pg.MultiAssignRef) *pg.Node {
	if row := m.Source.GetRowExpr(); row != nil && int(m.Colno) <= len(row.Args) {
		return row.Args[m.Colno-1]
	}
	return m.Source
}

// setProblems checks that a set clause stamps updated_at from the app.
func setProblems(write string, targets []*pg.Node) []string {
	for _, t := range targets {
		r := t.GetResTarget()
		if r.GetName() != "updated_at" {
			continue
		}
		v := r.Val
		if m := v.GetMultiAssignRef(); m != nil {
			v = assigned(m)
		}
		if from := source(v); from != "" {
			return []string{fmt.Sprintf("%s stamps updated_at from %s", write, from)}
		}
		return nil
	}
	return []string{write + " leaves updated_at"}
}

func table(r *pg.RangeVar) string {
	if r.Schemaname != "" {
		return r.Schemaname + "." + r.Relname
	}
	return r.Relname
}

// problems lists the writes in sql, those in CTEs too, that leave a stamp out or take it from the database.
func problems(sql string) []string {
	tree, err := pg.Parse(sql)
	if err != nil {
		if isWrite(sql) {
			return []string{fmt.Sprintf("%s: %v", unparsed, err)}
		}
		return nil
	}
	var out []string
	for _, s := range tree.Stmts {
		var inserts, updates []string
		each(s, func(m protoreflect.ProtoMessage) {
			switch m := m.(type) {
			case *pg.InsertStmt:
				inserts = append(inserts, insertProblems("an insert into "+table(m.Relation), m.Cols, rows(m.SelectStmt.GetSelectStmt()))...)
				if c := m.OnConflictClause; c.GetAction() == pg.OnConflictAction_ONCONFLICT_UPDATE {
					updates = append(updates, setProblems("an update of the conflicting row", c.TargetList)...)
				}
			case *pg.UpdateStmt:
				updates = append(updates, setProblems("an update of "+table(m.Relation), m.TargetList)...)
			case *pg.MergeStmt:
				for _, n := range m.MergeWhenClauses {
					switch w := n.GetMergeWhenClause(); w.CommandType {
					case pg.CmdType_CMD_INSERT:
						inserts = append(inserts, insertProblems("an insert into "+table(m.Relation), w.TargetList, [][]*pg.Node{w.Values})...)
					case pg.CmdType_CMD_UPDATE:
						updates = append(updates, setProblems("an update of "+table(m.Relation), w.TargetList)...)
					}
				}
			}
		})
		out = append(out, append(inserts, updates...)...)
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
		{"an update over lines", "UPDATE outbox\n\tSET published_at = $1\n\tWHERE event_id = $2", []string{"an update of outbox leaves updated_at"}},
		{"an update with double spaces", `UPDATE  t  SET  n = 1`, []string{"an update of t leaves updated_at"}},
		{"a comment inside a write", "UPDATE t /* the row */ SET n = 1 -- for now\n, updated_at = $1", nil},
		{"an aliased table", `UPDATE t AS x SET n = 1 WHERE x.id = $1`, []string{"an update of t leaves updated_at"}},
		{"an alias without as", `UPDATE t x SET n = 1, updated_at = $2 WHERE x.id = $1`, nil},
		{"a schema's table", `INSERT INTO public.t (id) VALUES ($1)`, []string{"an insert into public.t leaves out created_at", "an insert into public.t leaves out updated_at"}},
		{"a table filled in by fmt", `UPDATE %s SET n = 1`, []string{unparsed + `: syntax error at or near "%"`}},
		{"an insert into a table filled in by fmt", `INSERT INTO %s (id, created_at, updated_at) VALUES ($1, $2, $2)`, []string{unparsed + `: syntax error at or near "%"`}},
		{"an insert that ends at its table", `INSERT INTO `, []string{unparsed + ": syntax error at end of input"}},
		{"the database's clock in an update", `UPDATE t SET n = 1, updated_at = now() WHERE id = $1`, []string{"an update of t stamps updated_at from the database's clock"}},
		{"the database's clock in an insert", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, CURRENT_TIMESTAMP, $2)`, []string{"an insert into t stamps created_at from the database's clock"}},
		{"the database's clock elsewhere is fine", `UPDATE t SET n = 1, updated_at = $1 WHERE expires_at < now()`, nil},
		{"a stamped merge passes", `MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET n = s.n, updated_at = $1 WHEN NOT MATCHED THEN INSERT (id, created_at, updated_at) VALUES (s.id, $1, $1)`, nil},
		{"a merge stamps both its branches", `MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET n = s.n WHEN NOT MATCHED THEN INSERT (id, created_at) VALUES (s.id, $1)`,
			[]string{"an insert into t leaves out updated_at", "an update of t leaves updated_at"}},
		{"a merge into a table filled in by fmt", `MERGE INTO %s USING s ON true WHEN MATCHED THEN DELETE`, []string{unparsed + `: syntax error at or near "%"`}},
		{"sqlc's arguments are the app's", `UPDATE t SET n = sqlc.narg(n), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id)`, nil},
		{"an insert from a select", `INSERT INTO t (id, created_at, updated_at) SELECT $1, now(), $2`, []string{"an insert into t stamps created_at from the database's clock"}},
		{"an insert from another table", `INSERT INTO t (id, created_at, updated_at) SELECT u.id, $1, clock_timestamp() FROM u WHERE u.expires_at < now()`,
			[]string{"an insert into t stamps updated_at from the database's clock"}},
		{"an insert from a union", `INSERT INTO t (id, created_at, updated_at) SELECT $1, $2, $2 UNION ALL SELECT $3, pg_catalog.now(), $2`,
			[]string{"an insert into t stamps created_at from the database's clock"}},
		{"an insert from a select *", `INSERT INTO t (id, created_at, updated_at) SELECT * FROM u`, []string{"an insert into t stamps created_at from " + starred, "an insert into t stamps updated_at from " + starred}},
		{"a second values row", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, $2, $2), ($3, now(), $2)`, []string{"an insert into t stamps created_at from the database's clock"}},
		{"the clock as a cast string", `UPDATE t SET n = 1, updated_at = 'now'::timestamptz`, []string{"an update of t stamps updated_at from the database's clock"}},
		{"the clock as a bare string", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, $2, ' Now ')`, []string{"an insert into t stamps updated_at from the database's clock"}},
		{"a stamp left to its default", `INSERT INTO t (id, created_at, updated_at) VALUES ($1, DEFAULT, $2)`, []string{"an insert into t stamps created_at from the database's default"}},
		{"columns set together", `UPDATE t SET (n, updated_at) = ($1, now())`, []string{"an update of t stamps updated_at from the database's clock"}},
		{"a quoted stamp", `UPDATE t SET "updated_at" = $1`, nil},
		{"a stamp inside a string is no stamp", `UPDATE t SET note = 'a, updated_at = $1' WHERE id = $2`, []string{"an update of t leaves updated_at"}},
		{"a dash pair inside a string", `UPDATE t SET note = 'a--b', updated_at = $1`, nil},
		{"the writes in a CTE", `WITH moved AS (UPDATE t SET n = 1 RETURNING id) INSERT INTO u (id, created_at, updated_at) SELECT id, now(), $1 FROM moved`,
			[]string{"an insert into u stamps created_at from the database's clock", "an update of t leaves updated_at"}},
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
			for _, p := range problems(src[m[0]:end]) {
				t.Errorf("%s %s: %s", filepath.Base(file), src[m[2]:m[3]], p)
			}
		}
	}
}

type goWrite struct {
	at    token.Position
	sql   string
	whole bool // false when a part is only known at run time, held by a ? in sql
}

func (w goWrite) problems() []string {
	if !w.whole {
		return []string{runTime}
	}
	return problems(w.sql)
}

// operands flattens a string concatenation into its parts, each constant part whole.
func operands(info *types.Info, e ast.Expr) []ast.Expr {
	if info.Types[e].Value != nil {
		return []ast.Expr{e}
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return operands(info, e.X)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return append(operands(info, e.X), operands(info, e.Y)...)
		}
	}
	return []ast.Expr{e}
}

// goWrites finds the SQL writes in the hand-written Go of the packages matching pattern, each string expression read whole.
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
		var visit func(ast.Node) bool
		visit = func(n ast.Node) bool {
			switch n.(type) {
			case *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr:
			default:
				return true
			}
			e := n.(ast.Expr)
			if tv, ok := p.TypesInfo.Types[e]; !ok || !isString(tv.Type) {
				return true
			}
			var sql strings.Builder
			whole := true
			for _, part := range operands(p.TypesInfo, e) {
				if v := p.TypesInfo.Types[part].Value; v != nil {
					sql.WriteString(constant.StringVal(v))
					continue
				}
				whole = false
				sql.WriteString(" ? ")
				ast.Inspect(part, visit)
			}
			if isWrite(sql.String()) {
				found = append(found, goWrite{p.Fset.Position(e.Pos()), sql.String(), whole})
			}
			return false
		}
		for _, f := range p.Syntax {
			if !ast.IsGenerated(f) { // buf's, and sqlc's from the query files above
				ast.Inspect(f, visit)
			}
		}
	}
	return found, nil
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// The Go scan folds constants, so SQL split across concatenated literals and constants is read whole, and a part known only at run time is reported.
func TestTheGoScanReadsSQLAsItIsBuilt(t *testing.T) {
	found, err := goWrites(".", "./testdata/stamping")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		got = append(got, f.problems()...)
	}
	want := []string{"an update of outbox leaves updated_at", "an insert into keys leaves out updated_at", runTime}
	if !slices.Equal(got, want) {
		t.Fatalf("problems %q, want %q", got, want)
	}
}

// SQL written by hand in Go, such as the idempotency and outbox stores', is checked the same way.
func TestEveryHandWrittenWriteStampsItsRows(t *testing.T) {
	t.Parallel()
	found, err := goWrites("../..", "./...")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("found no hand-written write to check")
	}
	for _, f := range found {
		for _, p := range f.problems() {
			t.Errorf("%s: %s", f.at, p)
		}
	}
}
