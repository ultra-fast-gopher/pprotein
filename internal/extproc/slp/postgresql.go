package slp

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

var executeRecord = regexp.MustCompile(`^(.*?\bLOG:[ \t]+duration:[ \t]+[0-9]+(?:\.[0-9]+)?[ \t]+ms[ \t]+)execute[ \t]+.*?:[ \t]+`)

func normalizePostgreSQLLog(source io.Reader, dest io.Writer) error {
	reader := bufio.NewReader(source)
	for {
		// ReadString also handles SQL statements longer than Scanner's limit.
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			// PostgreSQL indents continuation lines. Do not rewrite SQL literals
			// that happen to contain text resembling an execute log header.
			if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
				line = executeRecord.ReplaceAllString(line, "${1}statement: ")
			}
			if _, writeErr := io.WriteString(dest, line); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
