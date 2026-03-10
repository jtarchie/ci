package server

import (
	terminal "github.com/buildkite/terminal-to-html/v3"
)

// ToTerminalHTML converts ANSI-encoded stdout to HTML using terminal-to-html.
// The returned HTML must be placed inside a <div class="term-container"> element.
func ToTerminalHTML(stdout string) string {
	return terminal.Render([]byte(stdout))
}
