package windowsengine

import (
	"fmt"
	"testing"
)

func TestSettings256Targets(t *testing.T) {
	s := valid()
	s.Targets = nil
	for i := 0; i < 256; i++ {
		s.Targets = append(s.Targets, fmt.Sprintf("198.18.0.%d/32", i))
	}
	if e := s.Validate(); e != nil {
		t.Fatalf("256 targets: %v", e)
	}
	s.Targets = append(s.Targets, "198.18.1.0/32")
	if s.Validate() == nil {
		t.Fatal("257 targets accepted")
	}
}
func TestSettingsRejectRouteMistakes(t *testing.T) {
	for _, routes := range [][]string{{}, {"198.18.0.1/24"}, {"198.18.0.0/24", "198.18.0.1/32"}, {"198.18.0.1/32", "198.18.0.1/32"}, {"::1/128"}} {
		s := valid()
		s.Targets = routes
		if s.Validate() == nil {
			t.Fatalf("accepted %v", routes)
		}
	}
}
