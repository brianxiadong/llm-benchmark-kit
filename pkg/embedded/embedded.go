// Package embedded provides embedded static files for the benchmark kit.
package embedded

import (
	_ "embed"
)

//go:embed text.txt
var TranscriptSample []byte

// GetTranscriptSample returns the embedded sample transcript text.
func GetTranscriptSample() []byte {
	return TranscriptSample
}
