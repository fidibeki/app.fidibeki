package membernotify

import "time"

// Window is the half-open range [Start, End) a digest covers. Half-open is what
// makes retries safe: an event belongs to exactly one window, and the next
// window starts precisely where the delivered one ended.
type Window struct {
	Start time.Time
	End   time.Time
}

const (
	// immediateLag holds a window open briefly so a transaction that commits
	// while the scheduler runs is picked up by the next window instead of being
	// missed by the current one.
	immediateLag = time.Minute
	// Lookback caps bound the work a single window can pull in after downtime.
	// They only take effect when nothing was delivered for that long, which in
	// practice means nothing happened, because a delivered window advances the
	// cursor to its own end.
	maxImmediateLookback = 6 * time.Hour
	maxDailyLookback     = 7 * 24 * time.Hour
	maxWeeklyLookback    = 31 * 24 * time.Hour
	// firstImmediateLookback keeps a member who just switched a category to
	// immediate from receiving a backlog on their first run.
	firstImmediateLookback = 15 * time.Minute
)

// NextWindow returns the window that is due for one cadence. cursor is the end of
// the member's last delivered window for that cadence; the zero value means the
// member has never been delivered one. The second result is false when nothing is
// due yet, which is the common case for digests between boundaries.
func NextWindow(preferences Preferences, cadence string, cursor, now time.Time) (Window, bool) {
	now = now.UTC()
	cursor = cursor.UTC()
	var end time.Time
	var firstLookback, maxLookback time.Duration
	switch cadence {
	case CadenceImmediate:
		end = now.Add(-immediateLag)
		firstLookback, maxLookback = firstImmediateLookback, maxImmediateLookback
	case CadenceDaily:
		end = dailyBoundary(preferences, now)
		firstLookback, maxLookback = 24*time.Hour, maxDailyLookback
	case CadenceWeekly:
		end = weeklyBoundary(preferences, now)
		firstLookback, maxLookback = 7*24*time.Hour, maxWeeklyLookback
	default:
		return Window{}, false
	}
	start := cursor
	if cursor.IsZero() {
		start = end.Add(-firstLookback)
	}
	if oldest := end.Add(-maxLookback); start.Before(oldest) {
		start = oldest
	}
	if !end.After(start) {
		return Window{}, false
	}
	return Window{Start: start, End: end}, true
}

// dailyBoundary is the most recent local digest hour that has already passed.
func dailyBoundary(preferences Preferences, now time.Time) time.Time {
	location := preferences.Location()
	local := now.In(location)
	boundary := time.Date(
		local.Year(), local.Month(), local.Day(), preferences.DigestHour, 0, 0, 0, location,
	)
	if boundary.After(local) {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary.UTC()
}

// weeklyBoundary is the most recent local digest weekday and hour that has
// already passed. Weekdays are ISO-8601: 1 = Monday through 7 = Sunday.
func weeklyBoundary(preferences Preferences, now time.Time) time.Time {
	location := preferences.Location()
	local := now.In(location)
	isoWeekday := int(local.Weekday())
	if isoWeekday == 0 {
		isoWeekday = 7
	}
	daysBack := isoWeekday - preferences.DigestWeekday
	if daysBack < 0 {
		daysBack += 7
	}
	boundary := time.Date(
		local.Year(), local.Month(), local.Day(), preferences.DigestHour, 0, 0, 0, location,
	).AddDate(0, 0, -daysBack)
	if boundary.After(local) {
		boundary = boundary.AddDate(0, 0, -7)
	}
	return boundary.UTC()
}
