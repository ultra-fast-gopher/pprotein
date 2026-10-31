package slp

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

var executeRecord = regexp.MustCompile(`^(.*?\bLOG:[ \t]+duration:[ \t]+[0-9]+(?:\.[0-9]+)?[ \t]+ms[ \t]+)execute[ \t]+.*?:[ \t]+`)
var emptyStatementRecord = regexp.MustCompile(`\bLOG:[ \t]+duration:[ \t]+[0-9]+(?:\.[0-9]+)?[ \t]+ms[ \t]+statement:[ \t\r\n]*$`)

func normalizePostgreSQLLog(source io.Reader, dest io.Writer) error {
	reader := bufio.NewReader(source)
	pendingHeader := ""
	for {
		// ReadString also handles SQL statements longer than Scanner's limit.
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if pendingHeader != "" {
				if strings.TrimSpace(line) == "" && err == nil {
					continue
				}
				if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
					line = pendingHeader + strings.TrimLeft(line, " \t")
				} else if _, writeErr := io.WriteString(dest, pendingHeader+"\n"); writeErr != nil {
					return writeErr
				}
				pendingHeader = ""
			}
			// PostgreSQL indents continuation lines. Do not rewrite SQL literals
			// that happen to contain text resembling an execute log header.
			if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
				line = executeRecord.ReplaceAllString(line, "${1}statement: ")
				if emptyStatementRecord.MatchString(line) {
					pendingHeader = strings.TrimRight(line, " \t\r\n") + " "
					line = ""
				}
			}
			if _, writeErr := io.WriteString(dest, line); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			if pendingHeader != "" {
				_, writeErr := io.WriteString(dest, pendingHeader+"\n")
				return writeErr
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}
