package slp

import (
	_ "embed"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/ultra-fast-gopher/pprotein/internal/collect"
	"github.com/ultra-fast-gopher/pprotein/internal/extproc"
	"github.com/ultra-fast-gopher/pprotein/internal/git"
	"github.com/ultra-fast-gopher/pprotein/internal/persistent"
	"github.com/ultra-fast-gopher/pprotein/internal/storage"
	"gopkg.in/yaml.v3"
)

type (
	snapshotAdder interface {
		AddSnapshot(*collect.SnapshotTarget, []byte, *git.RepositoryInfo) (*collect.Snapshot, error)
	}

	handler struct {
		opts    *collect.Options
		config  *persistent.Handler
		adder   snapshotAdder
		command string
	}
)

//go:embed slp.yml
var defaultConfig []byte

//go:embed pgslp.yml
var defaultPostgreSQLConfig []byte

func NewHandler(opts *collect.Options, store storage.Storage) (*handler, error) {
	return newHandler(opts, store, "my", "slp.yml", defaultConfig)
}

func NewPostgreSQLHandler(opts *collect.Options, store storage.Storage) (*handler, error) {
	return newHandler(opts, store, "pg", "pgslp.yml", defaultPostgreSQLConfig)
}

func newHandler(opts *collect.Options, store storage.Storage, command, configName string, defaults []byte) (*handler, error) {
	h := &handler{
		opts:    opts,
		command: command,
	}

	config, err := persistent.New(store, configName, defaults, h.sanitize)
	if err != nil {
		return nil, fmt.Errorf("failed to create targets: %w", err)
	}
	h.config = config

	return h, nil
}

func (h *handler) Register(g *echo.Group) error {
	h.config.RegisterHandlers(g.Group("/config"))

	api := extproc.NewHandler(&processor{confPath: h.config.GetPath(), command: h.command}, h.opts)
	if err := api.Register(g); err != nil {
		return fmt.Errorf("failed to register extproc handlers: %w", err)
	}
	h.adder = api
	return nil
}

func (h *handler) AddSnapshot(target *collect.SnapshotTarget, content []byte, repository *git.RepositoryInfo) (*collect.Snapshot, error) {
	return h.adder.AddSnapshot(target, content, repository)
}

func (h *handler) sanitize(raw []byte) ([]byte, error) {
	var config interface{}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	res, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}
	return res, nil
}
