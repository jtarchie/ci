package storage

import (
	"log/slog"
	"net/url"
)

type InitFunc func(string, string, *slog.Logger) (Driver, error)

var drivers = map[string]InitFunc{}

func Add(driverName string, init InitFunc) {
	drivers[driverName] = init
}

func Each(f func(string, InitFunc)) {
	for name, init := range drivers {
		f(name, init)
	}
}

func GetFromDSN(dsn string) (InitFunc, bool) {
	uri, err := url.Parse(dsn)
	if err != nil {
		return nil, false
	}

	driverName := uri.Scheme
	init, ok := drivers[driverName]

	return init, ok
}
