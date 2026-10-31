package capture

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	runtimepprof "runtime/pprof"
)

const (
	TypePprof     = "pprof"
	TypeHTTPLog   = "httplog"
	TypeSlowLog   = "slowlog"
	TypePGSlowLog = "pgslowlog"
)

type (
	Handler struct {
		mu      sync.Mutex
		active  map[string]*activeCapture
		logPath map[string]string
	}

	request struct {
		SessionID string
		Type      string
	}

	activeCapture struct {
		typ       string
		startedAt time.Time
		profile   *bytes.Buffer
		file      *os.File
		filePath  string
		fileInfo  os.FileInfo
		offset    int64
	}
)

func NewHandler(httplogPath, slowlogPath, pgslowlogPath string) *Handler {
	return &Handler{
		active: make(map[string]*activeCapture),
		logPath: map[string]string{
			TypeHTTPLog:   httplogPath,
			TypeSlowLog:   slowlogPath,
			TypePGSlowLog: pgslowlogPath,
		},
	}
}

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || len(req.SessionID) > 128 {
		http.Error(w, "SessionID is required (maximum 128 bytes)", http.StatusBadRequest)
		return
	}
	key := captureKey(req.Type, req.SessionID)

	h.mu.Lock()
	defer h.mu.Unlock()
	if existing, ok := h.active[key]; ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SessionID": req.SessionID,
			"Type":      req.Type,
			"StartedAt": existing.startedAt,
		})
		return
	}

	capture := &activeCapture{typ: req.Type, startedAt: time.Now().UTC()}
	switch req.Type {
	case TypePprof:
		capture.profile = &bytes.Buffer{}
		if err := runtimepprof.StartCPUProfile(capture.profile); err != nil {
			http.Error(w, fmt.Sprintf("could not start CPU profile: %v", err), http.StatusConflict)
			return
		}
	case TypeHTTPLog, TypeSlowLog, TypePGSlowLog:
		path := h.logPath[req.Type]
		file, err := os.Open(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not open log file: %v", err), http.StatusInternalServerError)
			return
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			http.Error(w, fmt.Sprintf("could not stat log file: %v", err), http.StatusInternalServerError)
			return
		}
		offset, err := file.Seek(0, io.SeekEnd)
		if err != nil {
			file.Close()
			http.Error(w, fmt.Sprintf("could not seek log file: %v", err), http.StatusInternalServerError)
			return
		}
		capture.file = file
		capture.filePath = path
		capture.fileInfo = info
		capture.offset = offset
	default:
		http.Error(w, "unsupported capture type", http.StatusBadRequest)
		return
	}

	h.active[key] = capture
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"SessionID": req.SessionID,
		"Type":      req.Type,
		"StartedAt": capture.startedAt,
	})
}

func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || len(req.SessionID) > 128 {
		http.Error(w, "SessionID is required (maximum 128 bytes)", http.StatusBadRequest)
		return
	}
	key := captureKey(req.Type, req.SessionID)

	h.mu.Lock()
	capture, ok := h.active[key]
	if !ok {
		h.mu.Unlock()
		http.Error(w, "capture not found", http.StatusNotFound)
		return
	}
	delete(h.active, key)

	var body []byte
	var err error
	if capture.typ == TypePprof {
		runtimepprof.StopCPUProfile()
		body = append([]byte(nil), capture.profile.Bytes()...)
	} else {
		body, err = h.readLogDelta(capture)
		_ = capture.file.Close()
	}
	startedAt := capture.startedAt
	h.mu.Unlock()

	if err != nil {
		http.Error(w, fmt.Sprintf("could not read captured data: %v", err), http.StatusInternalServerError)
		return
	}
	if len(body) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Capture-Started-At", startedAt.Format(time.RFC3339Nano))
	w.Header().Set("X-Capture-Ended-At", time.Now().UTC().Format(time.RFC3339Nano))
	w.Header().Set("X-Capture-Type", capture.typ)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) readLogDelta(capture *activeCapture) ([]byte, error) {
	info, err := capture.file.Stat()
	if err != nil {
		return nil, err
	}
	end := info.Size()
	start := capture.offset
	if end < start {
		// The log was truncated in place while this session was active.
		start = 0
	}

	body := &bytes.Buffer{}
	if end > start {
		if _, err := io.CopyN(body, io.NewSectionReader(capture.file, start, end-start), end-start); err != nil {
			return nil, err
		}
	}

	currentInfo, err := os.Stat(capture.filePath)
	if err == nil && !os.SameFile(capture.fileInfo, currentInfo) {
		// Include the active file after one rotation. The original descriptor
		// continues to refer to the file that was active at session start.
		current, openErr := os.Open(capture.filePath)
		if openErr != nil {
			return nil, openErr
		}
		defer current.Close()
		if _, copyErr := io.Copy(body, current); copyErr != nil {
			return nil, copyErr
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return body.Bytes(), nil
}

func (h *Handler) authorized(r *http.Request) bool {
	token := os.Getenv("PPROTEIN_CONTROL_TOKEN")
	if token == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	got := strings.TrimPrefix(header, "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func captureKey(typ, sessionID string) string {
	return typ + "\x00" + sessionID
}
