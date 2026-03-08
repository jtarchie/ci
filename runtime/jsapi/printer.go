package jsapi

import (
	"log/slog"

	"github.com/dop251/goja_nodejs/console"
)

// Printer implements console.Printer for the goja JS runtime.
type Printer struct {
	logger *slog.Logger
}

// NewPrinter creates a new Printer with the given logger.
func NewPrinter(logger *slog.Logger) *Printer {
	return &Printer{logger: logger}
}

func (p *Printer) Error(message string) {
	p.logger.Error("runtime.error", "message", message)
}

func (p *Printer) Log(message string) {
	p.logger.Info("runtime.info", "message", message)
}

func (p *Printer) Warn(message string) {
	p.logger.Warn("runtime.warn", "message", message)
}

var _ console.Printer = (*Printer)(nil)
