package mobile

import (
	"encoding/json"
	"testing"
)

func TestRestoreTrafficBudget(t *testing.T) {
	b, e := RestoreTrafficBudget(`{"epoch":"run","limit":100,"used":90,"reserved":5,"blocked":true,"by_class":{"user":80,"control":10}}`)
	if e != nil {
		t.Fatal(e)
	}
	var s map[string]any
	json.Unmarshal([]byte(b.Snapshot()), &s)
	if s["epoch"] != "run" || s["used"] != float64(90) || s["reserved"] != float64(0) || b.CellAllowed() {
		t.Fatal(s)
	}
	for _, raw := range []string{`{}`, `{"epoch":"x","limit":-1}`, `{"epoch":"x","used":1,"by_class":{"user":2}}`} {
		if _, e := RestoreTrafficBudget(raw); e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
