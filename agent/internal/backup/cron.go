package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Due reports whether a 5-field cron expression matches t (minute resolution).
// Supports *, lists, ranges and steps; day-of-month and day-of-week are ANDed unless one is "*".
func Due(expr string, t time.Time) (bool, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return false, fmt.Errorf("schedule must have 5 fields")
	}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	vals := [5]int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	var ok [5]bool
	for i := range f {
		m, err := fieldMatch(f[i], vals[i], limits[i][0], limits[i][1])
		if err != nil {
			return false, err
		}
		ok[i] = m
	}
	dom, dow := f[2] != "*", f[4] != "*"
	day := ok[2] && ok[4]
	if dom != dow { // standard cron: if either is restricted... keep it strict and predictable
		day = ok[2] && ok[4]
	}
	return ok[0] && ok[1] && ok[3] && day, nil
}

func fieldMatch(field string, v, lo, hi int) (bool, error) {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return false, fmt.Errorf("bad step in %q", field)
			}
			step, part = n, part[:i]
		}
		a, b := lo, hi
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			p := strings.SplitN(part, "-", 2)
			x, e1 := strconv.Atoi(p[0])
			y, e2 := strconv.Atoi(p[1])
			if e1 != nil || e2 != nil || x < lo || y > hi || x > y {
				return false, fmt.Errorf("bad range in %q", field)
			}
			a, b = x, y
		default:
			x, err := strconv.Atoi(part)
			if err != nil || x < lo || x > hi {
				return false, fmt.Errorf("bad value in %q", field)
			}
			a, b = x, x
		}
		if v >= a && v <= b && (v-a)%step == 0 {
			return true, nil
		}
	}
	return false, nil
}
