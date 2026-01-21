package sse

import (
	"strings"
	"testing"
)

func TestParser_BasicEvent(t *testing.T) {
	input := "data: hello world\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "hello world" {
		t.Errorf("expected data 'hello world', got '%s'", event.Data)
	}
}

func TestParser_MultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "line1\nline2\nline3"
	if event.Data != expected {
		t.Errorf("expected data '%s', got '%s'", expected, event.Data)
	}
}

func TestParser_DoneSignal(t *testing.T) {
	input := "data: [DONE]\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "[DONE]" {
		t.Errorf("expected [DONE], got '%s'", event.Data)
	}
}

func TestParser_Comment(t *testing.T) {
	input := ": this is a comment\ndata: actual data\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != "actual data" {
		t.Errorf("expected 'actual data', got '%s'", event.Data)
	}
}

func TestParser_EventWithID(t *testing.T) {
	input := "id: 123\ndata: test data\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.ID != "123" {
		t.Errorf("expected ID '123', got '%s'", event.ID)
	}
	if event.Data != "test data" {
		t.Errorf("expected data 'test data', got '%s'", event.Data)
	}
}

func TestParser_EventType(t *testing.T) {
	input := "event: message\ndata: test\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Event != "message" {
		t.Errorf("expected event 'message', got '%s'", event.Event)
	}
}

func TestParser_MultipleEvents(t *testing.T) {
	input := "data: first\n\ndata: second\n\ndata: third\n\n"
	parser := NewParser(strings.NewReader(input))

	expected := []string{"first", "second", "third"}
	for i, exp := range expected {
		event, err := parser.Next()
		if err != nil {
			t.Fatalf("event %d: unexpected error: %v", i, err)
		}
		if event.Data != exp {
			t.Errorf("event %d: expected '%s', got '%s'", i, exp, event.Data)
		}
	}
}

func TestParser_LargeData(t *testing.T) {
	// Test data larger than 64KB (the bufio.Scanner limit)
	largeData := strings.Repeat("x", 100*1024) // 100KB
	input := "data: " + largeData + "\n\n"
	parser := NewParser(strings.NewReader(input))

	event, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Data != largeData {
		t.Errorf("data length mismatch: expected %d, got %d", len(largeData), len(event.Data))
	}
}

func TestReadEvents(t *testing.T) {
	input := "data: one\n\ndata: two\n\ndata: three\n\n"
	events, err := ReadEvents(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func TestParseEventBlock(t *testing.T) {
	data := []byte("id: 42\nevent: update\ndata: hello\ndata: world\n")
	event := ParseEventBlock(data)

	if event.ID != "42" {
		t.Errorf("expected ID '42', got '%s'", event.ID)
	}
	if event.Event != "update" {
		t.Errorf("expected event 'update', got '%s'", event.Event)
	}
	if event.Data != "hello\nworld" {
		t.Errorf("expected data 'hello\\nworld', got '%s'", event.Data)
	}
}
