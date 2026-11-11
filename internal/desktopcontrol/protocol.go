package desktopcontrol

import (
	"bytes"
	"encoding/json"
	"io"
)

// decodeObject rejects duplicate and non-object JSON as well as trailing values.
func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, controlError("invalid_request")
	}
	obj := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, controlError("invalid_request")
		}
		key, ok := token.(string)
		if !ok {
			return nil, controlError("invalid_request")
		}
		if _, duplicate := obj[key]; duplicate {
			return nil, controlError("invalid_request")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, controlError("invalid_request")
		}
		obj[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, controlError("invalid_request")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, controlError("invalid_request")
	}
	return obj, nil
}

func decodeCommand(raw []byte) (Command, error) {
	var c Command
	if len(raw) > 4096 {
		return c, controlError("invalid_request")
	}
	obj, err := decodeObject(raw)
	if err != nil {
		return c, err
	}
	c.Op = jsonString(obj["op"])
	switch c.Op {
	case "status", "disconnect", "reconnect":
		if len(obj) != 1 {
			return c, controlError("invalid_request")
		}
	case "connect":
		if len(obj) != 2 {
			return c, controlError("invalid_request")
		}
		c.Profile = jsonString(obj["profile"])
		if !knownProfile(c.Profile) {
			return c, controlError("invalid_request")
		}
	case "autostart":
		if len(obj) != 2 {
			return c, controlError("invalid_request")
		}
		value := string(obj["enabled"])
		if value != "true" && value != "false" {
			return c, controlError("invalid_request")
		}
		b := value == "true"
		c.Enabled = &b
	default:
		return c, controlError("invalid_request")
	}
	return c, nil
}
