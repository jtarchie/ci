package runtime

import (
	"log/slog"

	"github.com/dop251/goja_nodejs/console"
)

type printer struct {
	logger *slog.Logger
}

func (p *printer) Error(message string) {
	p.logger.Error(message)
}

func (p *printer) Log(message string) {
	p.logger.Info(message)
}

func (p *printer) Warn(message string) {
	p.logger.Warn(message)
}

var _ console.Printer = (*printer)(nil)
