package runtime

import (
	"log/slog"

	"github.com/dop251/goja_nodejs/console"
)

type printer struct {
	logger *slog.Logger
}

func (p *printer) Error(message string) {
	p.logger.Error("runtime.error", "message", message)
}

func (p *printer) Log(message string) {
	p.logger.Info("runtime.info", "message", message)
}

func (p *printer) Warn(message string) {
	p.logger.Warn("runtime.warn", "message", message)
}

var _ console.Printer = (*printer)(nil)
