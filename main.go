package main

import (
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/cache/s3"
	_ "github.com/jtarchie/ci/orchestra/digitalocean"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/fly"
	_ "github.com/jtarchie/ci/orchestra/k8s"
	_ "github.com/jtarchie/ci/orchestra/native"
	_ "github.com/jtarchie/ci/orchestra/qemu"
	_ "github.com/jtarchie/ci/resources/git"
	_ "github.com/jtarchie/ci/resources/mock"
	_ "github.com/jtarchie/ci/secrets/local"
	_ "github.com/jtarchie/ci/storage/sqlite"
	"github.com/lmittmann/tint"
)

type CLI struct {
	Runner      commands.Runner      `cmd:"" help:"Run a pipeline"`
	Resource    commands.Resource    `cmd:"" help:"Execute a native resource operation"`
	Transpile   commands.Transpile   `cmd:"" help:"Transpile a pipeline"`
	Server      commands.Server      `cmd:"" help:"Run a server"`
	SetPipeline    commands.SetPipeline    `cmd:"" help:"Upload a pipeline to the server"  name:"set-pipeline"`
	DeletePipeline commands.DeletePipeline `cmd:"" help:"Delete a pipeline from the server" name:"delete-pipeline"`

	LogLevel  slog.Level `default:"info"             env:"CI_LOG_LEVEL"   help:"Set the log level (debug, info, warn, error)"`
	AddSource bool       `env:"CI_ADD_SOURCE"        help:"Add source code location to log messages"`
	LogFormat string     `default:"text"             env:"CI_LOG_FORMAT"  enum:"text,json" help:"Set the log format (text, json)"`
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
