package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDuration is time.ParseDuration plus a "d" suffix (days, 24h each),
// e.g. "90d" or "36h". Mixed forms like "1d12h" are not supported.
// time.ParseDuration stops at hours, and "2160h" is operator-hostile.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid duration %q (want e.g. 90d or 24h)", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q (want e.g. 90d or 24h)", s)
	}
	return d, nil
}
