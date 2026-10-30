package echov5

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestIntegrateRegistersDebugRoutes(t *testing.T) {
	e := echo.New()
	Integrate(e)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /debug/pprof/ status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
