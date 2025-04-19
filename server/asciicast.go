package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// AsciiCastVersion is the version of the asciicast format.
	AsciiCastVersion = 2
	// DefaultTerminalWidth is the default width of the terminal in columns.
	DefaultTerminalWidth = 80
	// DefaultTerminalHeight is the default height of the terminal in rows.
	DefaultTerminalHeight = 24
	// LinesPerChunk controls how many lines we group together in a single event.
	LinesPerChunk = 5
	// ChunkSpeedDivisor controls how the chunk size affects timing.
	ChunkSpeedDivisor = 1000.0
	// MinTimeIncrement is the minimum time between events in seconds.
	MinTimeIncrement = 0.05
	// MaxTimeIncrement is the maximum time between events in seconds.
	MaxTimeIncrement = 0.3
)

func ToAsciiCast(stdout string, writer io.Writer) error {
	// Create the header with required fields
	header := map[string]interface{}{
		"version":   AsciiCastVersion,
		"width":     DefaultTerminalWidth,
		"height":    DefaultTerminalHeight,
		"timestamp": time.Now().Unix(),
	}

	// Write the header as JSON object
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("could not marshal header: %w", err)
	}

	_, err = writer.Write(headerBytes)
	if err != nil {
		return fmt.Errorf("could not write header: %w", err)
	}

	_, err = writer.Write([]byte("\n"))
	if err != nil {
		return fmt.Errorf("could not write header newline: %w", err)
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
		currentChunk.WriteString("\n")

		lineCount++

		// Write chunk when it reaches the desired size or end of input
		if lineCount < LinesPerChunk {
			continue
		}

		if err := writeChunk(&currentChunk, writer, &currentTime); err != nil {
			return err
		}

		// Reset for next chunk
		currentChunk.Reset()

		lineCount = 0
	}

	// Handle any remaining content in the final chunk
	err = writeChunk(&currentChunk, writer, &currentTime)
	if err != nil {
		return fmt.Errorf("could not write final chunk: %w", err)
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stdout: %w", err)
	}

	return nil
}

// writeChunk writes a chunk of text as an asciicast event and updates the current time.
func writeChunk(chunk *strings.Builder, writer io.Writer, currentTime *float64) error {
	chunkStr := chunk.String()

	// Create and write event
	event := []interface{}{*currentTime, "o", chunkStr}

	eventBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("could not marshal event: %w", err)
	}

	_, err = writer.Write(eventBytes)
	if err != nil {
		return fmt.Errorf("could not write event: %w", err)
	}

	_, err = writer.Write([]byte("\n"))
	if err != nil {
		return fmt.Errorf("could not write event newline: %w", err)
	}

	// Calculate timing based on chunk length
	timeIncrement := float64(len(chunkStr)) / ChunkSpeedDivisor

	if timeIncrement < MinTimeIncrement {
		timeIncrement = MinTimeIncrement
	}

	if timeIncrement > MaxTimeIncrement {
		timeIncrement = MaxTimeIncrement
	}

	*currentTime += timeIncrement

	return nil
}
