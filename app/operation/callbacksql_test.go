package operation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var callbackAllowedSQL = []string{
	"SELECT $1", "insert into t(v) values ($1) returning v", "UPDATE t SET v=$1 RETURNING v", "DELETE FROM t WHERE v=$1",
	" \t\n\r\f/* outer /* inner */ end */-- line\rSELECT 'COMMIT; -- /*', \"a;\"\"b\", 'a''b'; /* tail */-- eof",
	"SELECT ($1 + ($23)), '/*';", "SELECT 'E''string', \"UESCAPE\", '$tag$'", "SELECT 1" + strings.Repeat(" ", 65528),
	"SELECT " + strings.Repeat("(", 64) + "1" + strings.Repeat(")", 64),
	strings.Repeat("/*", 32) + "x" + strings.Repeat("*/", 32) + " SELECT 1",
}

var callbackControlSQL = []string{
	"BEGIN", "BEGIN WORK", "BEGIN TRANSACTION", "START TRANSACTION", "COMMIT", "COMMIT WORK", "COMMIT TRANSACTION",
	"COMMIT AND CHAIN", "COMMIT AND NO CHAIN", "END", "END WORK", "END TRANSACTION AND CHAIN", "ROLLBACK", "ABORT",
	"ROLLBACK WORK", "ROLLBACK TRANSACTION", "ROLLBACK AND CHAIN", "ROLLBACK AND NO CHAIN", "ABORT WORK", "ABORT TRANSACTION",
	"ROLLBACK TO x", "ROLLBACK TO SAVEPOINT x", "SAVEPOINT x", "RELEASE x", "RELEASE SAVEPOINT x", "PREPARE TRANSACTION 'x'",
	"COMMIT PREPARED 'x'", "ROLLBACK PREPARED 'x'", "SET TRANSACTION READ ONLY", "SET LOCAL TRANSACTION READ ONLY",
	"SET SESSION TRANSACTION READ ONLY", "SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY",
}

func callbackRejectedSQL() []string {
	out := []string{
		"", " ", "-- only", "/* only */", "WITH x AS (SELECT 1) SELECT * FROM x", "VALUES(1)", "MERGE INTO t", "EXPLAIN SELECT 1",
		"CALL p()", "DO 'x'", "PREPARE x AS SELECT 1", "EXECUTE x", "COPY t TO STDOUT", "SET x=1", "RESET ALL", "DISCARD ALL",
		"SELECT 1; COMMIT", "SELECT 1;;", ";SELECT 1", "SELECT 'x", "SELECT \"x", "SELECT /* x", "SELECT (1;)", "SELECT (1", "SELECT 1)",
		"SELECT $$x$$", "SELECT $tag$x$tag$", "SELECT $é$x$é$", "SELECT $0", "SELECT $01", "SELECT $1name", "SELECT x$1", "SELECT 1$1", "SELECT $1$2", "SELECT $",
		"SELECT U&'x'", "SELECT U&\"x\"", "SELECT 'x' UESCAPE '!'", "SELECT E'x'", "SELECT b'1'", "SELECT X'aa'", "SELECT N'x'",
		"SELECT 'x\\'; COMMIT --'", "SELECT 1 /* \\ */", "SELECT '\\'", "\xef\xbb\xbfSELECT 1", "SELECT\u00a01", "SELECT\u200b1", "SELECT 'é'",
		"SELECT \x00", "SELECT \x7f", "SELECT \v1", "SELECT \x011", strings.Repeat(" ", 65537),
		"SELECT " + strings.Repeat("(", 65) + "1" + strings.Repeat(")", 65), strings.Repeat("/*", 33) + "x" + strings.Repeat("*/", 33) + " SELECT 1",
	}
	for _, q := range callbackControlSQL {
		out = append(out, q, " /* prefix */ "+strings.ToLower(q), "-- prefix\n"+strings.ReplaceAll(q, " ", "/**/"))
	}
	return append(append([]string{}, callbackControlSQL...), out...)
}

func TestCallbackSQLProfile(t *testing.T) {
	for _, q := range callbackAllowedSQL {
		if !callbackSQL(q) {
			t.Errorf("allowed SQL rejected: %.100q", q)
		}
	}
	for _, q := range callbackRejectedSQL() {
		if callbackSQL(q) {
			t.Errorf("unsupported SQL admitted: %.100q", q)
		}
	}
}

func TestCallbackSQLAllMethods(t *testing.T) {
	for _, method := range []string{"exec", "query", "row"} {
		t.Run(method, func(t *testing.T) {
			conn, db, invalidate := callbackUnitDB(t)
			for _, q := range callbackRejectedSQL() {
				conn.query = "not-called"
				var err error
				switch method {
				case "exec":
					_, err = db.ExecContext(context.Background(), q)
				case "query":
					var rows Rows
					rows, err = db.QueryContext(context.Background(), q)
					if rows != nil {
						t.Fatal("rejected query returned non-nil rows")
					}
				case "row":
					err = db.QueryRowContext(context.Background(), q).Scan(new(int))
				}
				if !errors.Is(err, errCallbackSQL) || conn.query != "not-called" {
					t.Fatalf("SQL rejection failed for %.100q: %v, driver called=%v", q, err, conn.query != "not-called")
				}
			}
			for _, q := range callbackAllowedSQL {
				var err error
				switch method {
				case "exec":
					_, err = db.ExecContext(context.Background(), q, "日本語")
				case "query":
					var rows Rows
					rows, err = db.QueryContext(context.Background(), q, "日本語")
					if err == nil {
						err = rows.Close()
					}
				case "row":
					err = db.QueryRowContext(context.Background(), q, "日本語").Scan(new(int))
				}
				if err != nil || conn.query != q || len(conn.args) != 1 || conn.args[0].Value != "日本語" {
					t.Fatalf("delegation changed query/args or failed: %v", err)
				}
			}
			if err := invalidate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func FuzzCallbackSQL(f *testing.F) {
	for _, q := range append(append([]string{}, callbackAllowedSQL...), callbackRejectedSQL()...) {
		f.Add(q)
	}
	f.Fuzz(func(t *testing.T, q string) {
		got := callbackSQL(q)
		if got != callbackSQL(q) {
			t.Fatal("non-deterministic scanner")
		}
		if !got {
			return
		}
		if len(q) < 1 || len(q) > 65536 {
			t.Fatal("size invariant")
		}
		for _, c := range []byte(q) {
			if c >= 127 || c == '\\' || c < 32 && !strings.ContainsRune("\t\n\r\f", rune(c)) {
				t.Fatal("byte invariant")
			}
		}
		// Independent lexical observer counts ordinary terminators and checks
		// the first word, rather than calling the production scanner helpers.
		words, terminators, trailing := callbackSQLObserve(q)
		if len(words) == 0 || !strings.Contains("|SELECT|INSERT|UPDATE|DELETE|", "|"+strings.ToUpper(words[0])+"|") || terminators > 1 || trailing {
			t.Fatal("statement invariant")
		}
	})
}

func callbackSQLObserve(q string) ([]string, int, bool) {
	var words []string
	terminators := 0
	trailing := false
	for len(q) > 0 {
		if terminators > 0 && !strings.ContainsRune(" \t\r\n\f", rune(q[0])) && !strings.HasPrefix(q, "--") && !strings.HasPrefix(q, "/*") {
			trailing = true
		}
		switch {
		case strings.HasPrefix(q, "--"):
			i := strings.IndexAny(q, "\r\n")
			if i < 0 {
				return words, terminators, trailing
			}
			q = q[i:]
		case strings.HasPrefix(q, "/*"):
			level := 1
			q = q[2:]
			for level > 0 && len(q) > 0 {
				if strings.HasPrefix(q, "/*") {
					level++
					q = q[2:]
				} else if strings.HasPrefix(q, "*/") {
					level--
					q = q[2:]
				} else {
					q = q[1:]
				}
			}
		case q[0] == '\'' || q[0] == '"':
			quote := q[0]
			q = q[1:]
			for len(q) > 0 {
				c := q[0]
				q = q[1:]
				if c == quote {
					if len(q) > 0 && q[0] == quote {
						q = q[1:]
						continue
					}
					break
				}
			}
		case q[0] == ';':
			terminators++
			q = q[1:]
		case strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_", rune(q[0])):
			i := 1
			for i < len(q) && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789", rune(q[i])) {
				i++
			}
			words = append(words, q[:i])
			q = q[i:]
		default:
			q = q[1:]
		}
	}
	return words, terminators, trailing
}
