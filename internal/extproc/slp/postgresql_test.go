package slp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNormalizePostgreSQLLog(t *testing.T) {
	prefix := "2026-10-31 01:00:00.000 UTC [42] postgres@bench LOG:  duration: 12.345 ms  "
	longQuery := "SELECT '" + strings.Repeat("x", 100000) + "';"
	for _, tt := range []struct{ name, input, want string }{
		{"simple", prefix + "statement: SELECT 1;\n", prefix + "statement: SELECT 1;\n"},
		{"unnamed", prefix + "execute <unnamed>: SELECT $1;\n", prefix + "statement: SELECT $1;\n"},
		{"named", prefix + "execute stmt:1: SELECT $1;\n", prefix + "statement: SELECT $1;\n"},
		{"multiline", prefix + "execute stmt: SELECT\n\t$1;\n", prefix + "statement: SELECT\n\t$1;\n"},
		{"parse", prefix + "parse stmt: SELECT $1;\n", prefix + "parse stmt: SELECT $1;\n"},
		{"bind", prefix + "bind stmt: SELECT $1;\n", prefix + "bind stmt: SELECT $1;\n"},
		{"continuation", "\t" + prefix + "execute stmt: literal\n", "\t" + prefix + "execute stmt: literal\n"},
		{"long and unterminated", prefix + "execute stmt: " + longQuery, prefix + "statement: " + longQuery},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var dest bytes.Buffer
			if err := normalizePostgreSQLLog(strings.NewReader(tt.input), &dest); err != nil {
				t.Fatal(err)
			}
			if dest.String() != tt.want {
				t.Fatalf("unexpected normalized log: got %q, want %q", dest.String(), tt.want)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestNormalizePostgreSQLLogWriteError(t *testing.T) {
	if err := normalizePostgreSQLLog(strings.NewReader("line\n"), failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("error = %v, want closed pipe", err)
	}
}
