package main

import (
	"net"
	"os"

	"github.com/ultra-fast-gopher/pprotein/integration/standalone"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "19000"
	}
	standalone.Integrate(net.JoinHostPort(os.Getenv("HOST"), port))
}
