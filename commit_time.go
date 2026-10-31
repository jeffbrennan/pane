package main

import (
	"fmt"
	"time"
)

func relativeCommitTime(date string, now time.Time) string {
	when, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return "unknown age"
	}
	seconds := max(int64(0), int64(now.Sub(when).Seconds()))
	for _, unit := range []struct {
		seconds int64
		label   string
	}{{365 * 24 * 60 * 60, "y"}, {30 * 24 * 60 * 60, "mo"}, {7 * 24 * 60 * 60, "w"}, {24 * 60 * 60, "d"}, {60 * 60, "h"}, {60, "m"}, {1, "s"}} {
		if seconds >= unit.seconds || unit.seconds == 1 {
			return fmt.Sprintf("%d%s ago", seconds/unit.seconds, unit.label)
		}
	}
	return "0s ago"
}

func historyCommitTime(date string, now time.Time) string {
	when, err := time.Parse(time.RFC3339, date)
	if err == nil && !when.After(now) && now.Sub(when) < 24*time.Hour {
		return relativeCommitTime(date, now)
	}
	if len(date) >= 10 {
		return date[:10]
	}
	return date
}
