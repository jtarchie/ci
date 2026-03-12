package server

import (
	"encoding/json"

	terminal "github.com/buildkite/terminal-to-html/v3"
)

type TerminalLogEntry struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func ParseTerminalLogs(raw any) []TerminalLogEntry {
	entries, ok := raw.([]any)
	if !ok {
		if rawJSON, ok := raw.(string); ok {
			var fromJSON []TerminalLogEntry
			if err := json.Unmarshal([]byte(rawJSON), &fromJSON); err == nil {
				return fromJSON
			}
		}

		return nil
	}

	logs := make([]TerminalLogEntry, 0, len(entries))
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		entryType, _ := entryMap["type"].(string)
		content, _ := entryMap["content"].(string)
		if entryType == "" || content == "" {
			continue
		}

		logs = append(logs, TerminalLogEntry{Type: entryType, Content: content})
	}

	return logs
}

func ToTerminalHTML(text string) string {
	return terminal.Render([]byte(text))
}

func ToTerminalHTMLFromLogs(logs []TerminalLogEntry) string {
	if len(logs) == 0 {
		return ""
	}

	var combined []byte
	for _, log := range logs {
		combined = append(combined, log.Content...)
	}

	return terminal.Render(combined)
}
