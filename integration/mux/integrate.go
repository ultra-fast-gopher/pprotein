package mux

import (
	"github.com/gorilla/mux"
	"github.com/ultra-fast-gopher/pprotein/integration"
)

func Integrate(r *mux.Router) {
	EnableDebugHandler(r)
	EnableDebugMode(r)
}

func EnableDebugHandler(r *mux.Router) {
	integration.RegisterDebugHandlers(r)
}

func EnableDebugMode(r *mux.Router) {
	return
}
