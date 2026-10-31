package group

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v4"
	"github.com/ultra-fast-gopher/pprotein/internal/collect"
	"github.com/ultra-fast-gopher/pprotein/internal/git"
	"github.com/ultra-fast-gopher/pprotein/internal/storage"
)

const (
	runBucket            = "runs"
	statusStarting       = "starting"
	statusRunning        = "running"
	statusStopping       = "stopping"
	statusCompleted      = "completed"
	statusPartialFailure = "partial_failure"
	statusFailed         = "failed"
	statusInterrupted    = "interrupted"
	maxCaptureBytes      = 512 << 20
)

type (
	SnapshotSink interface {
		AddSnapshot(*collect.SnapshotTarget, []byte, *git.RepositoryInfo) (*collect.Snapshot, error)
	}

	Run struct {
		ID              string
		Status          string
		StartedAt       time.Time
		StopRequestedAt *time.Time
		FinishedAt      *time.Time
		Targets         []RunTarget
	}

	RunTarget struct {
		Type       string
		Label      string
		URL        string
		ControlURL string
		Status     string
		Error      string
		StartedAt  *time.Time
		SnapshotID string
	}

	runManager struct {
		mu    sync.RWMutex
		store storage.Storage
		runs  map[string]*Run
		sinks map[string]SnapshotSink
	}
)

func newRunManager(store storage.Storage, sinks map[string]SnapshotSink) (*runManager, error) {
	m := &runManager{store: store, runs: make(map[string]*Run), sinks: sinks}
	rawRuns, err := store.GetAll(runBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to load runs: %w", err)
	}
	for _, raw := range rawRuns {
		run := &Run{}
		if err := json.Unmarshal(raw, run); err != nil {
			log.Printf("[!] unmarshalling run failed: %v", err)
			continue
		}
		if run.Status == statusStarting || run.Status == statusRunning || run.Status == statusStopping {
			run.Status = statusInterrupted
			for i := range run.Targets {
				if run.Targets[i].Status == "starting" {
					run.Targets[i].Status = "failed"
					run.Targets[i].Error = "pprotein restarted before the target start was confirmed"
				}
			}
		}
		m.runs[run.ID] = run
		if err := m.saveLocked(run); err != nil {
			return nil, fmt.Errorf("failed to persist recovered run %s: %w", run.ID, err)
		}
	}
	return m, nil
}

func (m *runManager) RegisterHandlers(g *echo.Group, owner *Collector) {
	g.GET("", owner.listRuns)
	g.POST("", owner.startRun)
	g.GET("/:id", owner.getRun)
	g.POST("/:id/stop", owner.stopRun)
}

func (cl *Collector) listRuns(c echo.Context) error {
	return c.JSON(http.StatusOK, cl.runs.list())
}

func (cl *Collector) getRun(c echo.Context) error {
	run := cl.runs.get(c.Param("id"))
	if run == nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}
	return c.JSON(http.StatusOK, run)
}

func (cl *Collector) startRun(c echo.Context) error {
	raw, err := cl.targets.GetContent()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to read targets: %v", err))
	}
	targets := []*CollectTarget{}
	if err := json.Unmarshal(raw, &targets); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to parse targets: %v", err))
	}
	if len(targets) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no targets configured")
	}
	prepared := make([]CollectTarget, len(targets))
	for i, target := range targets {
		if target.Type != "pprof" && target.Type != "httplog" && target.Type != "slowlog" && target.Type != "pgslowlog" {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("unsupported session target type: %s", target.Type))
		}
		if _, err := controlURL(*target, "start"); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid control URL for %s: %v", target.Label, err))
		}
		if cl.runs.sinks[target.Type] == nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("no snapshot processor registered for %s", target.Type))
		}
		prepared[i] = *target
	}

	run, err := cl.runs.create(prepared)
	if err != nil {
		if err == errActiveRun {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create run: %v", err))
	}
	go cl.runs.startTargets(run.ID)
	return c.JSON(http.StatusAccepted, run)
}

func (cl *Collector) stopRun(c echo.Context) error {
	run, started, err := cl.runs.requestStop(c.Param("id"))
	if err != nil {
		if err == errRunNotFound {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	if started {
		go cl.runs.stopTargets(run.ID)
		return c.JSON(http.StatusAccepted, run)
	}
	return c.JSON(http.StatusOK, run)
}

var (
	errActiveRun   = fmt.Errorf("a session is already active")
	errRunNotFound = fmt.Errorf("run not found")
)

func (m *runManager) create(targets []CollectTarget) (*Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, current := range m.runs {
		if current.Status == statusStarting || current.Status == statusRunning || current.Status == statusStopping || current.Status == statusInterrupted {
			return nil, errActiveRun
		}
	}

	now := time.Now().UTC()
	id := fmt.Sprintf("run-%x", now.UnixNano())
	for m.runs[id] != nil {
		id += "x"
	}
	run := &Run{
		ID:        id,
		Status:    statusStarting,
		StartedAt: now,
		Targets:   make([]RunTarget, len(targets)),
	}
	for i, target := range targets {
		run.Targets[i] = RunTarget{
			Type:       target.Type,
			Label:      target.Label,
			URL:        target.URL,
			ControlURL: target.ControlURL,
			Status:     "starting",
		}
	}
	if err := m.saveLocked(run); err != nil {
		return nil, err
	}
	m.runs[id] = run
	return cloneRun(run), nil
}

func (m *runManager) startTargets(id string) {
	run := m.get(id)
	if run == nil {
		return
	}
	var wg sync.WaitGroup
	for i := range run.Targets {
		i := i
		target := run.Targets[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.startTarget(id, i, target)
		}()
	}
	wg.Wait()
	m.update(id, func(run *Run) {
		started := 0
		for _, target := range run.Targets {
			if target.Status == "running" {
				started++
			}
		}
		if started > 0 {
			run.Status = statusRunning
		} else {
			run.Status = statusFailed
			finished := time.Now().UTC()
			run.FinishedAt = &finished
		}
	})
}

func (m *runManager) startTarget(id string, index int, target RunTarget) {
	endpoint, err := controlURL(CollectTarget{URL: target.URL, ControlURL: target.ControlURL}, "start")
	if err == nil {
		err = m.startControl(endpoint, id, target.Type)
	}
	m.update(id, func(run *Run) {
		if index >= len(run.Targets) {
			return
		}
		if err != nil {
			run.Targets[index].Status = "failed"
			run.Targets[index].Error = err.Error()
			return
		}
		started := time.Now().UTC()
		run.Targets[index].Status = "running"
		run.Targets[index].StartedAt = &started
	})
}

func (m *runManager) startControl(endpoint, id, typ string) error {
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		resp, err := m.controlRequest(ctx, endpoint, id, typ)
		if err != nil {
			cancel()
			if attempt == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			cancel()
			return fmt.Errorf("target returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		_ = resp.Body.Close()
		cancel()
		return nil
	}
	return fmt.Errorf("could not start target capture")
}

func (m *runManager) requestStop(id string) (*Run, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[id]
	if run == nil {
		return nil, false, errRunNotFound
	}
	switch run.Status {
	case statusStarting:
		return nil, false, fmt.Errorf("session is still starting")
	case statusStopping:
		return cloneRun(run), false, nil
	case statusRunning, statusInterrupted:
		stopRequested := time.Now().UTC()
		run.Status = statusStopping
		run.StopRequestedAt = &stopRequested
		if err := m.saveLocked(run); err != nil {
			return nil, false, err
		}
		return cloneRun(run), true, nil
	default:
		return cloneRun(run), false, nil
	}
}

func (m *runManager) stopTargets(id string) {
	run := m.get(id)
	if run == nil {
		return
	}
	var wg sync.WaitGroup
	for i := range run.Targets {
		if run.Targets[i].Status != "running" {
			continue
		}
		i := i
		target := run.Targets[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.stopTarget(id, i, target)
		}()
	}
	wg.Wait()
	m.update(id, func(run *Run) {
		complete := 0
		for _, target := range run.Targets {
			if target.Status == statusCompleted {
				complete++
			}
		}
		switch {
		case complete == len(run.Targets):
			run.Status = statusCompleted
		case complete > 0:
			run.Status = statusPartialFailure
		default:
			run.Status = statusFailed
		}
		finished := time.Now().UTC()
		run.FinishedAt = &finished
	})
}

func (m *runManager) stopTarget(id string, index int, target RunTarget) {
	endpoint, err := controlURL(CollectTarget{URL: target.URL, ControlURL: target.ControlURL}, "stop")
	var body []byte
	var repository *git.RepositoryInfo
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var resp *http.Response
		resp, err = m.controlRequest(ctx, endpoint, id, target.Type)
		if err == nil {
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusNoContent:
				err = fmt.Errorf("target produced no data")
			case http.StatusOK:
				if raw := resp.Header.Get("X-Git-Repository"); raw != "" {
					repository = &git.RepositoryInfo{}
					if err := json.Unmarshal([]byte(raw), repository); err != nil {
						repository = nil
					}
				}
				body, err = io.ReadAll(io.LimitReader(resp.Body, maxCaptureBytes+1))
				if err == nil && len(body) > maxCaptureBytes {
					err = fmt.Errorf("capture exceeds %d bytes", maxCaptureBytes)
				}
			default:
				message, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				err = fmt.Errorf("target returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
			}
		}
	}

	var snapshotID string
	if err == nil {
		sink := m.sinks[target.Type]
		if sink == nil {
			err = fmt.Errorf("no snapshot processor registered for %s", target.Type)
		}
	}
	if err == nil {
		startedAt := time.Now().UTC()
		if target.StartedAt != nil {
			startedAt = *target.StartedAt
		}
		duration := int(math.Ceil(time.Since(startedAt).Seconds()))
		if duration < 1 {
			duration = 1
		}
		created, addErr := m.sinks[target.Type].AddSnapshot(&collect.SnapshotTarget{
			GroupId:  id,
			Label:    target.Label,
			URL:      target.URL,
			Duration: duration,
		}, body, repository)
		if addErr != nil {
			err = fmt.Errorf("failed to process captured data: %w", addErr)
		} else {
			snapshotID = created.ID
		}
	}
	m.update(id, func(run *Run) {
		if index >= len(run.Targets) {
			return
		}
		if err != nil {
			run.Targets[index].Status = "failed"
			run.Targets[index].Error = err.Error()
			return
		}
		run.Targets[index].Status = statusCompleted
		run.Targets[index].SnapshotID = snapshotID
	})
}

func (m *runManager) controlRequest(ctx context.Context, endpoint, id, typ string) (*http.Response, error) {
	body, err := json.Marshal(map[string]string{"SessionID": id, "Type": typ})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("PPROTEIN_CONTROL_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

func controlURL(target CollectTarget, action string) (string, error) {
	var parsed *url.URL
	var err error
	if target.ControlURL != "" {
		parsed, err = url.Parse(target.ControlURL)
	} else {
		parsed, err = url.Parse(target.URL)
		if err == nil {
			marker := strings.Index(parsed.Path, "/debug/")
			if marker < 0 {
				return "", fmt.Errorf("URL path must contain /debug/ or ControlURL must be set")
			}
			parsed.Path = parsed.Path[:marker] + "/debug/capture"
			parsed.RawPath = ""
		}
	}
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("expected an absolute HTTP(S) URL")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + action
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (m *runManager) get(id string) *Run {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneRun(m.runs[id])
}

func (m *runManager) list() []*Run {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Run, 0, len(m.runs))
	for _, run := range m.runs {
		result = append(result, cloneRun(run))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.After(result[j].StartedAt)
	})
	return result
}

func (m *runManager) update(id string, update func(*Run)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[id]
	if run == nil {
		return
	}
	update(run)
	if err := m.saveLocked(run); err != nil {
		log.Printf("failed to persist run %s: %v", id, err)
	}
}

func (m *runManager) saveLocked(run *Run) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return m.store.Put(runBucket, run.ID, data)
}

func cloneRun(run *Run) *Run {
	if run == nil {
		return nil
	}
	data, err := json.Marshal(run)
	if err != nil {
		return nil
	}
	copy := &Run{}
	if err := json.Unmarshal(data, copy); err != nil {
		return nil
	}
	return copy
}
