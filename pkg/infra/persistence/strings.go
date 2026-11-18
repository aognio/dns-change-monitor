package persistence

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Value implements driver.Valuer.
func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	encoded, err := json.Marshal([]string(l))
	if err != nil {
		return nil, fmt.Errorf("encode string list: %w", err)
	}
	return string(encoded), nil
}

// Scan implements sql.Scanner.
func (l *StringList) Scan(src any) error {
	if src == nil {
		*l = nil
		return nil
	}
	var raw []byte
	switch value := src.(type) {
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		return fmt.Errorf("scan string list: unsupported type %T", src)
	}
	if len(raw) == 0 {
		*l = nil
		return nil
	}
	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode string list: %w", err)
	}
	*l = decoded
	return nil
}
