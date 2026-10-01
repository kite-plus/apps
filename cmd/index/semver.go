package main

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// version is a semantic version. A pre-release sorts before the release it
// leads to, and build metadata is ignored, as Kite compares them.
type version struct {
	major, minor, patch int
	pre                 string
}

// parseVersion reads the version of a release: major.minor.patch, perhaps
// with a pre-release, so that it names one tag and one archive.
func parseVersion(s string) (version, bool) { return parse(s, true) }

// parse reads a version; unless full, a missing minor or patch counts as
// zero, as Kite reads the versions in a range.
func parse(s string, full bool) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 3 || (full && len(parts) != 3) || (hasPre && pre == "") {
		return version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || (full && len(p) > 1 && p[0] == '0') {
			return version{}, false
		}
		n[i] = v
	}
	return version{major: n[0], minor: n[1], patch: n[2], pre: pre}, true
}

func (v version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != "" {
		s += "-" + v.pre
	}
	return s
}

func (v version) compare(w version) int {
	if c := cmp.Compare(v.major, w.major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.minor, w.minor); c != 0 {
		return c
	}
	if c := cmp.Compare(v.patch, w.patch); c != 0 {
		return c
	}
	switch {
	case v.pre == w.pre:
		return 0
	case v.pre == "":
		return 1
	case w.pre == "":
		return -1
	}
	a, b := strings.Split(v.pre, "."), strings.Split(w.pre, ".")
	for i := range min(len(a), len(b)) {
		x, xerr := strconv.Atoi(a[i])
		y, yerr := strconv.Atoi(b[i])
		switch {
		case xerr == nil && yerr == nil:
			if c := cmp.Compare(x, y); c != 0 {
				return c
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(a[i], b[i]); c != 0 {
				return c
			}
		}
	}
	return cmp.Compare(len(a), len(b))
}

// satisfies reports whether a version meets a range written as a theme's or
// a plugin's requires is, read as Kite reads it: comparisons separated by
// spaces or commas must all hold, alternatives are separated by ||, and a
// version with no comparison must be met exactly. An empty range admits
// every version.
func satisfies(expr string, v version) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	met := false
	for alt := range strings.SplitSeq(expr, "||") {
		fields := strings.Fields(strings.ReplaceAll(alt, ",", " "))
		if len(fields) == 0 {
			return false, fmt.Errorf("%q has an empty alternative", expr)
		}
		all := true
		for _, f := range fields {
			op := "="
			for _, candidate := range []string{">=", "<=", ">", "<", "="} {
				if rest, ok := strings.CutPrefix(f, candidate); ok {
					op, f = candidate, rest
					break
				}
			}
			w, ok := parse(f, false)
			if !ok {
				return false, fmt.Errorf("%q in %q is not a version", f, expr)
			}
			c := v.compare(w)
			switch op {
			case ">=":
				all = all && c >= 0
			case ">":
				all = all && c > 0
			case "<=":
				all = all && c <= 0
			case "<":
				all = all && c < 0
			default:
				all = all && c == 0
			}
		}
		met = met || all
	}
	return met, nil
}
