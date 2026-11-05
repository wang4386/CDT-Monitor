package scheduler

import (
	"testing"
	"time"
)

func TestParseAcceptsFiveFieldCronOnly(t *testing.T) {
	for _, expression := range []string{"0 0 1 * *", "15 22 1,15 * *", "*/10 8-17 * * 1-5"} {
		if _, err := Parse(expression); err != nil {
			t.Errorf("Parse(%q): %v", expression, err)
		}
	}
	for _, expression := range []string{"", "@monthly", "0 0 0 1 * *", "61 0 * * *"} {
		if _, err := Parse(expression); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", expression)
		}
	}
}

func TestNextOccurrencesUseRequestedTimezone(t *testing.T) {
	from := time.Date(2026, time.October, 17, 8, 0, 0, 0, time.UTC)
	got, err := NextOccurrences("0 0 1 * *", "Asia/Shanghai", from, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Format("2006-01-02 15:04 -0700") != "2026-11-01 00:00 +0800" || got[1].Format("2006-01-02 15:04 -0700") != "2026-12-01 00:00 +0800" {
		t.Fatalf("unexpected occurrences: %v", got)
	}
}

func TestLastOccurrenceWithinReturnsLatestMissedMinute(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 17, 8, 5, 20, 0, location)
	got, ok := LastOccurrenceWithin("* * * * *", now, 10*time.Minute)
	if !ok || !got.Equal(time.Date(2026, time.October, 17, 8, 5, 0, 0, location)) {
		t.Fatalf("got %v, ok=%t", got, ok)
	}
	if _, ok = LastOccurrenceWithin("0 0 1 * *", now, 10*time.Minute); ok {
		t.Fatal("monthly plan should not be due on this date")
	}
}
