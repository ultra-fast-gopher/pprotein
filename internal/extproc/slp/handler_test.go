package slp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/ultra-fast-gopher/pprotein/internal/collect"
	"github.com/ultra-fast-gopher/pprotein/internal/event"
	"github.com/ultra-fast-gopher/pprotein/internal/storage"
)

func TestDatabaseProcessorsRemainSeparate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test SLP executable uses a POSIX shell")
	}
	binDir := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s\\n' \"$1\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = --file ]; then cat \"$2\"; break; fi\n  shift\ndone\n"
	if err := os.WriteFile(filepath.Join(binDir, "slp"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	input := []byte("2026-10-31 01:00:00.000 UTC [42] LOG:  duration: 1.000 ms  execute <unnamed>: SELECT $1;\n")
	for _, typ := range []string{"slowlog", "pgslowlog"} {
		opts := &collect.Options{Type: typ, Ext: ".log", Store: store, EventHub: event.NewHub()}
		newHandler := NewHandler
		command, configName, record := "my", "slp.yml", "execute <unnamed>:"
		if typ == "pgslowlog" {
			newHandler = NewPostgreSQLHandler
			command, configName, record = "pg", "pgslp.yml", "statement:"
		}
		h, err := newHandler(opts, store)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(h.config.GetPath()) != configName {
			t.Fatalf("%s uses wrong config: %s", typ, h.config.GetPath())
		}
		if err := h.Register(e.Group("/api/" + typ)); err != nil {
			t.Fatal(err)
		}
		snapshot, err := h.AddSnapshot(&collect.SnapshotTarget{Label: typ}, input, nil)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/"+typ+"/"+snapshot.ID, nil))
		if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Body.String(), command+"\n") || !strings.Contains(recorder.Body.String(), record) {
			t.Fatalf("%s processed report: code=%d body=%q", typ, recorder.Code, recorder.Body.String())
		}
		recorder = httptest.NewRecorder()
		e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/"+typ+"/data/"+snapshot.ID, nil))
		body, err := io.ReadAll(recorder.Result().Body)
		if err != nil || recorder.Code != http.StatusOK || string(body) != string(input) {
			t.Fatalf("%s raw log was modified: code=%d body=%q err=%v", typ, recorder.Code, body, err)
		}
	}
}
