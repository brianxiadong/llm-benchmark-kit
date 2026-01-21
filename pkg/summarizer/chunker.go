// Package summarizer provides meeting transcript summarization functionality.
package summarizer

import (
	"strings"
	"unicode/utf8"
)

// Chunker splits text into chunks of specified size.
type Chunker struct {
	MaxChunkSize int // Maximum characters per chunk
}

// NewChunker creates a new Chunker with the specified max chunk size.
func NewChunker(maxChunkSize int) *Chunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 8000
	}
	return &Chunker{MaxChunkSize: maxChunkSize}
}

// Split splits the text into chunks, preferring natural paragraph boundaries.
func (c *Chunker) Split(text string) []string {
	// Split by double newlines (paragraphs)
	paragraphs := c.splitByParagraphs(text)

	// Combine paragraphs into chunks within size limit
	return c.combineIntochunks(paragraphs)
}

// splitByParagraphs splits text by paragraph boundaries.
func (c *Chunker) splitByParagraphs(text string) []string {
	// Try double newline first
	parts := strings.Split(text, "\n\n")

	var paragraphs []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			paragraphs = append(paragraphs, part)
		}
	}

	return paragraphs
}

// combineIntoChunks combines paragraphs into chunks within the size limit.
func (c *Chunker) combineIntochunks(paragraphs []string) []string {
	if len(paragraphs) == 0 {
		return nil
	}

	var chunks []string
	var currentChunk strings.Builder

	for _, para := range paragraphs {
		paraLen := utf8.RuneCountInString(para)
		currentLen := utf8.RuneCountInString(currentChunk.String())

		// If single paragraph exceeds max size, split it further
		if paraLen > c.MaxChunkSize {
			// First, flush current chunk if not empty
			if currentLen > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
			// Split large paragraph by lines
			chunks = append(chunks, c.splitLargeParagraph(para)...)
			continue
		}

		// Check if adding this paragraph exceeds limit
		if currentLen+paraLen+2 > c.MaxChunkSize { // +2 for \n\n separator
			if currentLen > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
		}

		// Add paragraph to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(para)
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

// splitLargeParagraph splits a paragraph that exceeds max size by lines.
func (c *Chunker) splitLargeParagraph(para string) []string {
	lines := strings.Split(para, "\n")

	var chunks []string
	var currentChunk strings.Builder

	for _, line := range lines {
		lineLen := utf8.RuneCountInString(line)
		currentLen := utf8.RuneCountInString(currentChunk.String())

		// If single line exceeds max, just add it as a chunk
		if lineLen > c.MaxChunkSize {
			if currentLen > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
			chunks = append(chunks, line)
			continue
		}

		if currentLen+lineLen+1 > c.MaxChunkSize {
			if currentLen > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n")
		}
		currentChunk.WriteString(line)
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}
