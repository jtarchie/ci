package orchestra

import "log/slog"

type InitFunc func(string, *slog.Logger) (Driver, error)

var drivers = map[string]InitFunc{}

func Add(driverName string, init InitFunc) {
	drivers[driverName] = init
}

func Each(f func(string, InitFunc)) {
	for name, init := range drivers {
		f(name, init)
	}
}

func Get(driverName string) (InitFunc, bool) {
	init, ok := drivers[driverName]

	return init, ok
}
