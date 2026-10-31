package slp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/ultra-fast-gopher/pprotein/internal/collect"
)

type (
	processor struct {
		confPath string
		command  string
	}
)

func (p *processor) Cacheable() bool {
	return true
}

func (p *processor) Process(snapshot *collect.Snapshot) (io.ReadCloser, error) {
	bodyPath, err := snapshot.BodyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find snapshot body: %w", err)
	}

	if p.command == "pg" {
		// SLP recognizes statement: records, while prepared statements are
		// logged as execute <name>:. Keep the stored log intact for downloads.
		source, err := os.Open(bodyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open PostgreSQL log: %w", err)
		}
		defer source.Close()
		normalized, err := os.CreateTemp("", "pprotein-pgslowlog-*.log")
		if err != nil {
			return nil, fmt.Errorf("failed to create normalized log: %w", err)
		}
		defer os.Remove(normalized.Name())
		if err := normalizePostgreSQLLog(source, normalized); err != nil {
			normalized.Close()
			return nil, fmt.Errorf("failed to normalize PostgreSQL log: %w", err)
		}
		if err := normalized.Close(); err != nil {
			return nil, fmt.Errorf("failed to close normalized log: %w", err)
		}
		bodyPath = normalized.Name()
	}

	output := "standard"
	if p.command == "pg" {
		output = "simple" // PostgreSQL logs do not contain MySQL lock/row counters.
	}
	cmd := exec.Command("slp", p.command, "--config", p.confPath, "--output", output, "--format", "tsv", "--file", bodyPath)

	res, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("external process aborted: %w", err)
	}

	return io.NopCloser(bytes.NewBuffer(res)), nil
}
