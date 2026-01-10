package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// AsciiCastConverter handles the conversion of text output to asciicast format.
type AsciiCastConverter struct {
	// Version is the version of the asciicast format
	Version int
	// TerminalWidth is the width of the terminal in columns
	TerminalWidth int
	// TerminalHeight is the height of the terminal in rows
	TerminalHeight int
	// LinesPerChunk controls how many lines we group together in a single event
	LinesPerChunk int
	// ChunkSpeedDivisor controls how the chunk size affects timing
	ChunkSpeedDivisor float64
	// MinTimeIncrement is the minimum time between events in seconds
	MinTimeIncrement float64
	// MaxTimeIncrement is the maximum time between events in seconds
	MaxTimeIncrement float64
}

// NewAsciiCastConverter creates a new converter with default settings.
func NewAsciiCastConverter() *AsciiCastConverter {
	//nolint:mnd
	return &AsciiCastConverter{
		Version:           2,
		TerminalWidth:     80,
		TerminalHeight:    24,
		LinesPerChunk:     5,
		ChunkSpeedDivisor: 1000.0,
		MinTimeIncrement:  0.05,
		MaxTimeIncrement:  0.3,
	}
}

// ToAsciiCast converts a string of stdout to asciicast format and writes it to the given writer.
func (c *AsciiCastConverter) ToAsciiCast(stdout string, writer io.Writer) error {
	// Create a single JSON encoder to be used throughout the function
	encoder := json.NewEncoder(writer)

	// Create the header with required fields
	header := map[string]interface{}{
		"version":   c.Version,
		"width":     c.TerminalWidth,
		"height":    c.TerminalHeight,
		"timestamp": time.Now().Unix(),
	}

	// Write the header using the encoder
	err := encoder.Encode(header)
	if err != nil {
		return fmt.Errorf("could not encode header: %w", err)
	}

	// Set up scanner to read line by line
	scanner := bufio.NewScanner(strings.NewReader(stdout))

	// Variables for chunk management
	var currentChunk strings.Builder

	lineCount := 0
	currentTime := 0.1 // Start at 0.1 second

	// Process each line
	for scanner.Scan() {
		line := scanner.Text()

		// Add line to current chunk
		currentChunk.WriteString(line)
		currentChunk.WriteString("\r\n")

		lineCount++

		// Write chunk when it reaches the desired size
		if lineCount >= c.LinesPerChunk {
			err := c.writeChunkWithEncoder(&currentChunk, encoder, &currentTime)
			if err != nil {
				return err
			}

			// Reset for next chunk
			currentChunk.Reset()

			lineCount = 0
		}
	}

	err = c.writeChunkWithEncoder(&currentChunk, encoder, &currentTime)
	if err != nil {
		return fmt.Errorf("could not write final chunk: %w", err)
	}

	// Check for scanner errors
	err = scanner.Err()
	if err != nil {
		return fmt.Errorf("error reading stdout: %w", err)
	}

	return nil
}

// writeChunkWithEncoder writes a chunk of text as an asciicast event using the provided JSON encoder
// and updates the current time.
func (c *AsciiCastConverter) writeChunkWithEncoder(chunk *strings.Builder, encoder *json.Encoder, currentTime *float64) error {
	chunkStr := chunk.String()

	// Create the event as a slice
	event := []interface{}{*currentTime, "o", chunkStr}

	// Encode the event (this will automatically add a newline)
	err := encoder.Encode(event)
	if err != nil {
		return fmt.Errorf("could not encode event: %w", err)
	}

	// Calculate timing based on chunk length
	timeIncrement := float64(len(chunkStr)) / c.ChunkSpeedDivisor

	if timeIncrement < c.MinTimeIncrement {
		timeIncrement = c.MinTimeIncrement
	}

	if timeIncrement > c.MaxTimeIncrement {
		timeIncrement = c.MaxTimeIncrement
	}

	*currentTime += timeIncrement

	return nil
}

// For backward compatibility.
func ToAsciiCast(stdout string, writer io.Writer) error {
	converter := NewAsciiCastConverter()

	return converter.ToAsciiCast(stdout, writer)
}
