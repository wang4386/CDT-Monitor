package scheduler

import (
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

var fiveFieldParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func Parse(expression string) (cron.Schedule, error) {
	return fiveFieldParser.Parse(expression)
}

func NextOccurrences(expression, timezone string, from time.Time, count int) ([]time.Time, error) {
	schedule, err := Parse(expression)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	if count < 1 || count > 10 {
		return nil, errors.New("occurrence count must be between 1 and 10")
	}
	next := from.In(location)
	occurrences := make([]time.Time, 0, count)
	for range count {
		next = schedule.Next(next)
		if next.IsZero() {
			return nil, errors.New("cron expression has no future occurrences")
		}
		occurrences = append(occurrences, next)
	}
	return occurrences, nil
}

func LastOccurrenceWithin(expression string, now time.Time, window time.Duration) (time.Time, bool) {
	schedule, err := Parse(expression)
	if err != nil || expression == "" {
		return time.Time{}, false
	}
	currentMinute := now.Truncate(time.Minute)
	cutoff := currentMinute.Add(-window).Truncate(time.Minute)
	next := schedule.Next(cutoff.Add(-time.Minute))
	var last time.Time
	for !next.IsZero() && !next.After(currentMinute) {
		if !next.Before(cutoff) {
			last = next
		}
		next = schedule.Next(next)
	}
	return last, !last.IsZero()
}
