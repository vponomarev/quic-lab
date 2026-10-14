package windowsengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func ParseSettings(raw []byte) (Settings, error) {
	var s Settings
	if len(raw) > 1<<20 {
		return s, errors.New("engine settings too large")
	}
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil {
		return Settings{}, errors.New("invalid engine settings JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Settings{}, errors.New("multiple settings documents")
	}
	if e := s.Validate(); e != nil {
		return Settings{}, e
	}
	return s, nil
}

// FieldError contains only stable codes and row numbers, never credential values.
type FieldError struct {
	Field string `json:"field"`
	Row   int    `json:"row,omitempty"`
	Code  string `json:"code"`
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Code }
