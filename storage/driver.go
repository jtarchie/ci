package storage

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type Driver interface {
	Close() error
	Set(prefix string, payload any) error
	GetAll(prefix string, fields []string) ([]Result, error)
}

type Payload map[string]any

func (p *Payload) Value() (driver.Value, error) {
	return json.Marshal(p) //nolint: wrapcheck
}

func (p *Payload) Scan(value any) error {
	//nolint: wrapcheck,err113
	switch x := value.(type) {
	case string:
		return json.NewDecoder(bytes.NewBufferString(x)).Decode(p)
	case []byte:
		return json.NewDecoder(bytes.NewBuffer(x)).Decode(p)
	case nil:
		return nil
	default:
		return fmt.Errorf("cannot scan type %T: %v", value, value)
	}
}

type Result struct {
	ID      int     `db:"id"`
	Path    string  `db:"path"`
	Payload Payload `db:"payload"`
}
