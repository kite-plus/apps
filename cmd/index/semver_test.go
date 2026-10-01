package main

import "testing"

func TestAReleaseVersionIsWrittenInFull(t *testing.T) {
	for s, want := range map[string]bool{
		"1.2.3": true, "v1.2.3": true, "1.2.3-rc.1": true, "1.2.3+build.5": true,
		"1.2": false, "01.2.3": false, "1.2.3-": false, "": false, "v": false,
		"1.2.3.4": false, "a.b.c": false, "1.-2.3": false,
	} {
		if _, ok := parseVersion(s); ok != want {
			t.Errorf("parseVersion(%q) = %v, want %v", s, ok, want)
		}
	}
}

func TestVersionsSortAsSemverSays(t *testing.T) {
	order := []string{
		"0.9.0", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.10.0",
	}
	for i := 1; i < len(order); i++ {
		a, _ := parseVersion(order[i-1])
		b, _ := parseVersion(order[i])
		if a.compare(b) >= 0 || b.compare(a) <= 0 {
			t.Errorf("%s should sort before %s", order[i-1], order[i])
		}
	}
	if v, _ := parseVersion("v1.2.3+build"); v.String() != "1.2.3" {
		t.Errorf("v1.2.3+build reads as %s", v)
	}
}

func TestRequiresIsReadAsKiteReadsIt(t *testing.T) {
	for _, c := range []struct {
		expr, v string
		want    bool
	}{
		{"", "0.1.0", true},
		{">=0.1.4 <2.0.0", "0.1.4", true},
		{">=0.1.4 <2.0.0", "0.1.3", false},
		{">=0.1.4 <2.0.0", "2.0.0", false},
		{">=0.1.4, <2", "1.9.9", true},
		{">=1", "1.0.0", true},
		{"0.1", "0.1.0", true},
		{"=0.1", "0.1.1", false},
		{"<0.1 || >=1.0", "0.5.0", false},
		{"<0.1 || >=1.0", "1.2.0", true},
		{">0.1.4", "0.1.5-rc.1", true},
		{">=1.0.0", "1.0.0-rc.1", false},
	} {
		v, _ := parseVersion(c.v)
		got, err := satisfies(c.expr, v)
		if err != nil || got != c.want {
			t.Errorf("satisfies(%q, %s) = %v, %v; want %v", c.expr, c.v, got, err, c.want)
		}
	}
	for _, bad := range []string{"~1.2", "^1.0.0", ">=", "1.0 ||", ">=x", "1.2.3.4"} {
		if _, err := satisfies(bad, version{}); err == nil {
			t.Errorf("satisfies(%q) read it as a range", bad)
		}
	}
}
