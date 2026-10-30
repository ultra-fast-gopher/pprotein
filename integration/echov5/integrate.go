// Package echov5 provides helpers for exposing pprotein's debug endpoints on Echo v5.
package echov5

import (
	"github.com/ultra-fast-gopher/pprotein/integration"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// Integrate registers pprotein's debug handler and the usual debug middleware.
func Integrate(e *echo.Echo) {
	EnableDebugHandler(e)
	EnableDebugMode(e)
}

// EnableDebugHandler exposes pprotein's debug routes under /debug/.
func EnableDebugHandler(e *echo.Echo) {
	e.Any("/debug/*", echo.WrapHandler(integration.NewDebugHandler()))
}

// EnableDebugMode installs request logging and panic recovery middleware.
func EnableDebugMode(e *echo.Echo) {
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
}
