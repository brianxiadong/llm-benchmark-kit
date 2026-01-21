// Package sse provides a robust SSE (Server-Sent Events) parser.
// This implementation uses bufio.Reader to avoid the 64K limit of bufio.Scanner.
package sse

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// Event represents a single SSE event.
type Event struct {
	ID    string // Event ID (optional)
	Event string // Event type (optional)
	Data  string // Event data (combined from multiple data: lines)
	Retry int    // Retry interval in ms (optional)
}

// Parser parses SSE events from an io.Reader.
type Parser struct {
	reader *bufio.Reader
}

// NewParser creates a new SSE parser.
func NewParser(r io.Reader) *Parser {
	return &Parser{
		reader: bufio.NewReader(r),
	}
}

// Next reads the next SSE event from the stream.
// Returns io.EOF when the stream ends.
func (p *Parser) Next() (*Event, error) {
	var event Event
	var dataLines []string

	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Handle any remaining data
				if len(dataLines) > 0 {
					event.Data = strings.Join(dataLines, "\n")
					return &event, nil
				}
				return nil, io.EOF
			}
			return nil, err
		}

		// Remove trailing newline
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		// Empty line = event boundary
		if line == "" {
			if len(dataLines) > 0 || event.ID != "" || event.Event != "" {
				event.Data = strings.Join(dataLines, "\n")
				return &event, nil
			}
			continue
		}

		// Comment line (keep-alive)
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			// Field with no value
			p.parseField(&event, &dataLines, line, "")
		} else {
			field := line[:colonIdx]
			value := line[colonIdx+1:]
			// Remove leading space from value (per SSE spec)
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
			p.parseField(&event, &dataLines, field, value)
		}
	}
}

func (p *Parser) parseField(event *Event, dataLines *[]string, field, value string) {
	switch field {
	case "id":
		event.ID = value
	case "event":
		event.Event = value
	case "data":
		*dataLines = append(*dataLines, value)
	case "retry":
		// Parse retry value (not commonly used)
	}
}

// ReadEvents reads all events from the stream and returns them.
// This is useful for testing but not recommended for production streaming.
func ReadEvents(r io.Reader) ([]*Event, error) {
	parser := NewParser(r)
	var events []*Event

	for {
		event, err := parser.Next()
		if err == io.EOF {
			return events, nil
		}
		if err != nil {
			return events, err
		}
		events = append(events, event)
	}
}

// ParseEventBlock parses a complete event block (data between \n\n).
// This is useful when you already have the complete event data.
func ParseEventBlock(data []byte) *Event {
	var event Event
	var dataLines []string

	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		lineStr := string(bytes.TrimSuffix(line, []byte("\r")))
		if lineStr == "" || strings.HasPrefix(lineStr, ":") {
			continue
		}

		colonIdx := strings.Index(lineStr, ":")
		if colonIdx == -1 {
			continue
		}

		field := lineStr[:colonIdx]
		value := lineStr[colonIdx+1:]
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}

		switch field {
		case "id":
			event.ID = value
		case "event":
			event.Event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}

	event.Data = strings.Join(dataLines, "\n")
	return &event
}
