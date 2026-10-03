package vlessserver

import (
	"bytes"
	"encoding/json"
	"io"
)

// ReadConfig validates private typed configuration without opening credentials or listeners.
func ReadConfig(path string) (Config, error) {
	var c Config
	raw, e := readMaterial(path, true)
	if e != nil || len(raw) > 128*1024 {
		return Config{}, errConfig
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || dec.Decode(new(any)) != io.EOF || c.Validate() != nil {
		return Config{}, errConfig
	}
	return c, nil
}
