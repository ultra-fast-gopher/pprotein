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
	// record holds the current log record without its line terminator so that
	// continuation lines can be folded into it.
	record, recordEnd := "", ""
	inRecord := false
	flush := func() error {
		if !inRecord {
			return nil
		}
		inRecord = false
		_, err := io.WriteString(dest, record+recordEnd)
		return err
	}
	for {
		// ReadString also handles SQL statements longer than Scanner's limit.
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			content := strings.TrimRight(line, "\r\n")
			end := line[len(content):]
			continuation := strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ")
			switch {
			case inRecord && (continuation || strings.TrimSpace(content) == ""):
				// PostgreSQL indents continuation lines. SLP copies newlines into its
				// TSV output verbatim, so fold multiline statements into one line.
				if part := strings.TrimSpace(content); part != "" {
					record = strings.TrimRight(record, " \t") + " " + part
				}
				recordEnd = end
			case continuation:
				// Orphaned continuation of a record that started before the capture.
				if _, writeErr := io.WriteString(dest, line); writeErr != nil {
					return writeErr
				}
			default:
				if writeErr := flush(); writeErr != nil {
					return writeErr
				}
				// Do not rewrite SQL literals that happen to contain text resembling
				// an execute log header; only record headers reach this point.
				record, recordEnd, inRecord = executeRecord.ReplaceAllString(content, "${1}statement: "), end, true
			}
		}
		if err == io.EOF {
			return flush()
		}
		if err != nil {
			return err
		}
	}
}
