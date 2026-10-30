package main

import (
	"os"

	"github.com/ultra-fast-gopher/pprotein/integration/standalone"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "19000"
	}
	standalone.Integrate(":" + port)
}
