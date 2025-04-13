package main

import (
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	"github.com/lmittmann/tint"
)

type CLI struct {
	Runner    commands.Runner    `cmd:"" help:"Run a pipeline"`
	Transpile commands.Transpile `cmd:"" help:"Transpile a pipeline"`
	Server    commands.Server    `cmd:"" help:"Run a server"`

	LogLevel  slog.Level `default:"info"                                  help:"Set the log level (debug, info, warn, error)"`
	AddSource bool       `help:"Add source code location to log messages"`
	LogFormat string     `default:"text"                                  enum:"text,json"                                    help:"Set the log format (text, json)"`
}

func main() {
	cli := &CLI{}
	ctx := kong.Parse(cli)

	if cli.LogFormat == "json" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level:     cli.LogLevel,
			AddSource: cli.AddSource,
		})))
	} else {
		slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, &tint.Options{
			Level:     cli.LogLevel,
			AddSource: cli.AddSource,
		})))
	}

	err := ctx.Run(slog.Default())
	ctx.FatalIfErrorf(err)
}
