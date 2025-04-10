package storage

type Driver interface {
	Close() error
	Set(prefix string, payload any) error
}
