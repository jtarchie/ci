package storage

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

type Driver interface {
	Close() error
	Set(prefix string, payload any) error
	GetAll(prefix string, fields []string) (Results, error)
}

type Payload map[string]any

func (p *Payload) Value() (driver.Value, error) {
	contents, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("could not marshal payload: %w", err)
	}

	return contents, nil
}

func (p *Payload) Scan(sqlValue any) error {
	switch typedValue := sqlValue.(type) {
	case string:
		err := json.NewDecoder(bytes.NewBufferString(typedValue)).Decode(p)
		if err != nil {
			return fmt.Errorf("could not unmarshal string payload: %w", err)
		}

		return nil
	case []byte:
		err := json.NewDecoder(bytes.NewBuffer(typedValue)).Decode(p)
		if err != nil {
			return fmt.Errorf("could not unmarshal byte payload: %w", err)
		}

		return nil
	case nil:
		return nil
	default:
		return fmt.Errorf("%w: cannot scan type %T: %v", errors.ErrUnsupported, sqlValue, sqlValue)
	}
}

type Result struct {
	ID      int     `db:"id"`
	Path    string  `db:"path"`
	Payload Payload `db:"payload"`
}

type Results []Result

func (results Results) AsTree() *Tree[Payload] {
	tree := NewTree[Payload]()
	for _, result := range results {
		tree.AddNode(result.Path, result.Payload)
	}

	tree.Flatten()

	return tree
}
