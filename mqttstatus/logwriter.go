package mqttstatus

import (
	"bytes"
	"regexp"
	"strings"
	"time"
)

// stdPrefix is the standard logger's date and time, removed from a line
// without a level tag (MMDVMHost's format has its own timestamp)
var stdPrefix = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? `)

// The gateway's log levels, lowest first, and MMDVMHost's letters for them
var (
	levelRank   = map[string]int{"DEBUG": 1, "INFO": 2, "WARN": 3, "ERROR": 4}
	levelLetter = map[string]string{"DEBUG": "D", "INFO": "I", "WARN": "W", "ERROR": "E"}
)

// LogWriter is an io.Writer for the standard logger that sends each line
// at or above Config.LogLevel to <Name>/log, in MMDVMHost's format:
//
//	I: 2026-11-20 01:23:45.678 text
//
// in UTC. A line's level is its "[LEVEL]" tag; a line without one counts
// as INFO.
type LogWriter struct {
	p   *Publisher
	min int // 0: nothing is sent
	now func() time.Time
}

// LogWriter returns the Publisher's log writer.
func (p *Publisher) LogWriter() *LogWriter {
	return &LogWriter{p: p, min: levelRank[strings.ToUpper(p.cfg.LogLevel)], now: time.Now}
}

// Write sends one log message. The standard logger calls Write once per
// message, so a multi-line message (a DEBUG dump of a frame, say) is one
// MQTT message with the level of its first line, not a line per line.
func (w *LogWriter) Write(b []byte) (int, error) {
	if w.min == 0 {
		return len(b), nil
	}
	if out, ok := w.format(string(bytes.TrimRight(b, "\n"))); ok {
		w.p.logLine(out)
	}
	return len(b), nil
}

// format turns a standard log line ("2026/11/19 20:11:55.785402 [INFO]
// text") into MMDVMHost's, or reports that its level is below the minimum.
func (w *LogWriter) format(line string) (string, bool) {
	level, text := "INFO", stdPrefix.ReplaceAllString(line, "")
	first, _, _ := strings.Cut(line, "\n")
	if i := strings.Index(first, "["); i >= 0 {
		if j := strings.Index(first[i:], "]"); j > 0 {
			if _, ok := levelRank[line[i+1:i+j]]; ok {
				level = line[i+1 : i+j]
				text = strings.TrimSpace(line[i+j+1:])
			}
		}
	}
	if levelRank[level] < w.min || text == "" {
		return "", false
	}
	return levelLetter[level] + ": " + w.now().UTC().Format("2006-01-02 15:04:05.000") + " " + text, true
}
