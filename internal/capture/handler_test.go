package capture

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogSessionCapturesOnlyAppendedBytes(t *testing.T) {
	logPath := t.TempDir() + "/http.log"
	if err := os.WriteFile(logPath, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(logPath, "")

	start := captureRequest(handler.Start, "session-1", TypeHTTPLog, "")
	if start.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, want %d: %s", start.Code, http.StatusAccepted, start.Body.String())
	}
	duplicate := captureRequest(handler.Start, "session-1", TypeHTTPLog, "")
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate start status = %d, want %d", duplicate.Code, http.StatusOK)
	}
	if err := os.WriteFile(logPath, []byte("before\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stop := captureRequest(handler.Stop, "session-1", TypeHTTPLog, "")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want %d: %s", stop.Code, http.StatusOK, stop.Body.String())
	}
	if got, want := stop.Body.String(), "after\n"; got != want {
		t.Fatalf("captured body = %q, want %q", got, want)
	}
	if got := stop.Header().Get("X-Capture-Type"); got != TypeHTTPLog {
		t.Fatalf("X-Capture-Type = %q, want %q", got, TypeHTTPLog)
	}
}

func TestPprofSessionReturnsGzipProfile(t *testing.T) {
	handler := NewHandler("", "")
	start := captureRequest(handler.Start, "profile-1", TypePprof, "")
	if start.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, want %d: %s", start.Code, http.StatusAccepted, start.Body.String())
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	var value uint64
	for time.Now().Before(deadline) {
		value = value*1664525 + 1013904223
	}
	if value == 0 {
		t.Fatal("CPU workload did not run")
	}

	stop := captureRequest(handler.Stop, "profile-1", TypePprof, "")
	if stop.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want %d: %s", stop.Code, http.StatusOK, stop.Body.String())
	}
	reader, err := gzip.NewReader(bytes.NewReader(stop.Body.Bytes()))
	if err != nil {
		t.Fatalf("profile is not gzip encoded: %v", err)
	}
	defer reader.Close()
	profile, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip profile: %v", err)
	}
	if len(profile) == 0 {
		t.Fatal("profile is empty")
	}
}

func TestControlTokenRequiredWhenConfigured(t *testing.T) {
	t.Setenv("PPROTEIN_CONTROL_TOKEN", "test-token")
	logPath := t.TempDir() + "/slow.log"
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("", logPath)

	unauthorized := captureRequest(handler.Start, "session-2", TypeSlowLog, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized start status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	start := captureRequest(handler.Start, "session-2", TypeSlowLog, "test-token")
	if start.Code != http.StatusAccepted {
		t.Fatalf("authorized start status = %d, want %d: %s", start.Code, http.StatusAccepted, start.Body.String())
	}
	stop := captureRequest(handler.Stop, "session-2", TypeSlowLog, "test-token")
	if stop.Code != http.StatusNoContent {
		t.Fatalf("empty capture stop status = %d, want %d", stop.Code, http.StatusNoContent)
	}
}

func captureRequest(handler http.HandlerFunc, sessionID, typ, token string) *httptest.ResponseRecorder {
	body := `{"SessionID":"` + sessionID + `","Type":"` + typ + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
