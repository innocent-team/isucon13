package main

import (
	"database/sql/driver"
	"github.com/goccy/go-json"
	"fmt"
)

type Int64ArrayJson []int64

func (a *Int64ArrayJson) Scan(src any) error {
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("invalid type: %T", src)
	}

	var arr []int64
	if err := json.Unmarshal(b, &arr); err != nil {
		return fmt.Errorf("failed to unmarshal: %w", err)
	}
	*a = arr

	return nil
}

func (a Int64ArrayJson) Value() (driver.Value, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}

	// https://blog.utgw.net/entry/2023/09/04/231719
	return string(b), nil
}
