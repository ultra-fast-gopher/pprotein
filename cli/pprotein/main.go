package main

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/ultra-fast-gopher/pprotein/integration/echov4"
	"github.com/ultra-fast-gopher/pprotein/internal/collect"
	"github.com/ultra-fast-gopher/pprotein/internal/collect/group"
	"github.com/ultra-fast-gopher/pprotein/internal/event"
	"github.com/ultra-fast-gopher/pprotein/internal/extproc/alp"
	"github.com/ultra-fast-gopher/pprotein/internal/extproc/slp"
	"github.com/ultra-fast-gopher/pprotein/internal/memo"
	"github.com/ultra-fast-gopher/pprotein/internal/pprof"
	"github.com/ultra-fast-gopher/pprotein/internal/storage"
	"github.com/ultra-fast-gopher/pprotein/view"
)

func start() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}

	store, err := storage.New("data")
	if err != nil {
		return err
	}

	e := echo.New()
	echov4.Integrate(e)

	fs, err := view.FS()
	if err != nil {
		return err
	}
	e.GET("/*", echo.WrapHandler(http.FileServer(http.FS(fs))))

	api := e.Group("/api", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", "no-store")
			return next(c)
		}
	})

	hub := event.NewHub()
	hub.RegisterHandlers(api.Group("/event"))

	pprofOpts := &collect.Options{
		Type:     "pprof",
		Ext:      "-pprof.pb.gz",
		Store:    store,
		EventHub: hub,
	}
	pprofHandler := pprof.NewHandler(pprofOpts)
	if err := pprofHandler.Register(api.Group("/pprof")); err != nil {
		return err
	}

	alpOpts := &collect.Options{
		Type:     "httplog",
		Ext:      "-httplog.log",
		Store:    store,
		EventHub: hub,
	}
	alpHandler, err := alp.NewHandler(alpOpts, store)
	if err != nil {
		return err
	}
	if err := alpHandler.Register(api.Group("/httplog")); err != nil {
		return err
	}

	slpOpts := &collect.Options{
		Type:     "slowlog",
		Ext:      "-slowlog.log",
		Store:    store,
		EventHub: hub,
	}
	slpHandler, err := slp.NewHandler(slpOpts, store)
	if err != nil {
		return err
	}
	if err := slpHandler.Register(api.Group("/slowlog")); err != nil {
		return err
	}

	pgslpOpts := &collect.Options{
		Type:     "pgslowlog",
		Ext:      "-pgslowlog.log",
		Store:    store,
		EventHub: hub,
	}
	pgslpHandler, err := slp.NewPostgreSQLHandler(pgslpOpts, store)
	if err != nil {
		return err
	}
	if err := pgslpHandler.Register(api.Group("/pgslowlog")); err != nil {
		return err
	}

	memoOpts := &collect.Options{
		Type:     "memo",
		Ext:      "-memo.log",
		Store:    store,
		EventHub: hub,
	}
	if err := memo.NewHandler(memoOpts).Register(api.Group("/memo")); err != nil {
		return err
	}

	grp, err := group.NewCollector(store, port, map[string]group.SnapshotSink{
		"pprof":     pprofHandler,
		"httplog":   alpHandler,
		"slowlog":   slpHandler,
		"pgslowlog": pgslpHandler,
	})
	if err != nil {
		return err
	}
	grp.RegisterHandlers(api.Group("/group"))
	grp.RegisterRunHandlers(api.Group("/runs"))

	return e.Start(":" + port)
}

func main() {
	if err := start(); err != nil {
		panic(err)
	}
}
