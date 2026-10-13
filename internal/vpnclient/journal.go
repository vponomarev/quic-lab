package vpnclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
)

// JournalStore must atomically persist and flush Save before returning success.
// A platform store provides ACLs and durability; fake stores are test-only.
type JournalStore interface {
	Load(context.Context) (Journal, error)
	Save(context.Context, Journal) error
}
type Journal struct {
	Version int            `json:"version"`
	Owner   string         `json:"owner,omitempty"`
	Entries []JournalEntry `json:"entries,omitempty"`
}
type JournalEntry struct {
	Change   Change `json:"change"`
	Applied  bool   `json:"applied"`
	Intent   bool   `json:"intent"`
	Reverted bool   `json:"reverted"`
}

var ErrJournal = errors.New("invalid_or_unavailable_network_journal")

func validateJournal(j Journal) error {
	if j.Version != 1 {
		return ErrJournal
	}
	if j.Owner == "" && len(j.Entries) == 0 {
		return nil
	}
	p := NetworkPlan{Owner: j.Owner}
	for _, e := range j.Entries {
		if (e.Reverted || e.Applied) && !e.Intent {
			return ErrJournal
		}
		p.Changes = append(p.Changes, e.Change)
	}
	if p.validate() != nil {
		return ErrJournal
	}
	return nil
}
func EncodeJournal(j Journal) ([]byte, error) {
	if j.Version == 0 && j.Owner == "" && len(j.Entries) == 0 {
		j.Version = 1
	}
	if e := validateJournal(j); e != nil {
		return nil, e
	}
	b, err := json.Marshal(j)
	if err != nil || len(b) > 1<<20 {
		return nil, ErrJournal
	}
	return b, nil
}
func DecodeJournal(b []byte) (Journal, error) {
	if len(b) > 1<<20 {
		return Journal{}, ErrJournal
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var j Journal
	if d.Decode(&j) != nil {
		return Journal{}, ErrJournal
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Journal{}, ErrJournal
	}
	if e := validateJournal(j); e != nil {
		return Journal{}, e
	}
	return j, nil
}
