package awg

import "testing"

func TestIPv4UnionCoverage(t *testing.T) {
	for _, c := range []struct {
		target  string
		allowed []string
		want    bool
	}{
		{"192.0.2.0/24", []string{"192.0.2.128/25", "192.0.2.0/25"}, true},
		{"192.0.2.0/24", []string{"192.0.2.0/25", "192.0.2.128/26"}, false},
		{"0.0.0.0/0", []string{"128.0.0.0/1", "0.0.0.0/1"}, true},
		{"255.255.255.255/32", []string{"128.0.0.0/1"}, true},
		{"192.0.2.0/24", []string{"0.0.0.0/0", "invalid"}, false},
		{"192.0.2.1/24", []string{"0.0.0.0/0"}, false},
		{"::/0", []string{"::/0"}, false},
		{"192.0.2.0/24", nil, false},
		{"192.0.2.0/24", []string{"192.0.2.0/25", "192.0.2.0/25"}, false},
	} {
		if got := CoversIPv4(c.allowed, c.target); got != c.want {
			t.Errorf("%s: got %v want %v", c.target, got, c.want)
		}
	}
}
