package membernotify

import (
	"testing"
	"time"
)

func lisbonPreferences(hour, weekday int) Preferences {
	preferences := Defaults()
	preferences.DigestHour = hour
	preferences.DigestWeekday = weekday
	preferences.Timezone = "Europe/Lisbon"
	return preferences
}

func TestDailyWindowFollowsTheMemberTimezone(t *testing.T) {
	t.Parallel()
	preferences := lisbonPreferences(8, 1)
	// 07:30 UTC is 08:30 in Lisbon summer time, so the 08:00 boundary has passed.
	now := time.Date(2026, 7, 20, 7, 30, 0, 0, time.UTC)
	window, due := NextWindow(preferences, CadenceDaily, time.Time{}, now)
	if !due {
		t.Fatal("daily digest was not due after its local boundary")
	}
	wantEnd := time.Date(2026, 7, 20, 7, 0, 0, 0, time.UTC)
	if !window.End.Equal(wantEnd) {
		t.Fatalf("window end = %s, want %s", window.End, wantEnd)
	}
	if window.Start != window.End.Add(-24*time.Hour) {
		t.Fatalf("first window = [%s, %s), want a 24 hour span", window.Start, window.End)
	}
	// The same boundary must not fire twice: the cursor from the delivered
	// window leaves nothing due until tomorrow's boundary.
	if _, due := NextWindow(preferences, CadenceDaily, window.End, now.Add(time.Hour)); due {
		t.Fatal("a delivered daily window came due again inside the same day")
	}
	next, due := NextWindow(preferences, CadenceDaily, window.End, now.Add(24*time.Hour))
	if !due {
		t.Fatal("the following day's digest was not due")
	}
	if !next.Start.Equal(window.End) {
		t.Fatalf("windows are not contiguous: %s then [%s, %s)", window.End, next.Start, next.End)
	}
}

func TestWeeklyWindowFollowsTheConfiguredWeekday(t *testing.T) {
	t.Parallel()
	// Friday is ISO weekday 5.
	preferences := lisbonPreferences(9, 5)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC) // Monday
	window, due := NextWindow(preferences, CadenceWeekly, time.Time{}, now)
	if !due {
		t.Fatal("weekly digest was not due")
	}
	boundary := window.End.In(window.End.UTC().Location())
	if boundary.Weekday() != time.Friday {
		t.Fatalf("weekly boundary fell on %s, want Friday", boundary.Weekday())
	}
	if window.End.After(now) {
		t.Fatalf("weekly boundary %s is in the future relative to %s", window.End, now)
	}
	if _, due := NextWindow(preferences, CadenceWeekly, window.End, now); due {
		t.Fatal("a delivered weekly window came due again before the next weekday")
	}
}

func TestImmediateWindowHoldsBackInFlightWrites(t *testing.T) {
	t.Parallel()
	preferences := Defaults()
	preferences.Timezone = "UTC"
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	cursor := now.Add(-5 * time.Minute)
	window, due := NextWindow(preferences, CadenceImmediate, cursor, now)
	if !due {
		t.Fatal("immediate window was not due")
	}
	if !window.Start.Equal(cursor) {
		t.Fatalf("window start = %s, want the cursor %s", window.Start, cursor)
	}
	if !window.End.Equal(now.Add(-immediateLag)) {
		t.Fatalf("window end = %s, want %s", window.End, now.Add(-immediateLag))
	}
	// A first run must not mail a backlog.
	first, due := NextWindow(preferences, CadenceImmediate, time.Time{}, now)
	if !due || first.End.Sub(first.Start) != firstImmediateLookback {
		t.Fatalf("first immediate window = [%s, %s)", first.Start, first.End)
	}
}

// Retrying the same window must produce the same range, or a retry could send a
// second copy of some events and skip others.
func TestWindowsAreDeterministicAndBounded(t *testing.T) {
	t.Parallel()
	preferences := lisbonPreferences(8, 1)
	cursor := time.Date(2026, 7, 19, 7, 0, 0, 0, time.UTC)
	first, _ := NextWindow(preferences, CadenceDaily, cursor, time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC))
	retry, _ := NextWindow(preferences, CadenceDaily, cursor, time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC))
	if first != retry {
		t.Fatalf("retry produced a different window: %+v then %+v", first, retry)
	}
	stale := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bounded, due := NextWindow(preferences, CadenceDaily, stale, time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC))
	if !due {
		t.Fatal("a long-stale cursor produced no window")
	}
	if span := bounded.End.Sub(bounded.Start); span > maxDailyLookback {
		t.Fatalf("stale window span = %s, want at most %s", span, maxDailyLookback)
	}
}

func TestUnknownCadenceIsNeverDue(t *testing.T) {
	t.Parallel()
	if _, due := NextWindow(Defaults(), "hourly", time.Time{}, time.Now()); due {
		t.Fatal("an unsupported cadence produced a window")
	}
}
