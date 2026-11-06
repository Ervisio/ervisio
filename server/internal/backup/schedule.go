package backup

import "time"

// Monthly boundaries advance from the previous due date, not from a delayed
// run's month. Jan 31 -> Feb 28 -> Mar 31 remains true after a March 1 catch-up.
func nextDue(c *Config, now time.Time) time.Time {
	due, err := time.Parse(time.RFC3339, c.NextDue)
	if err != nil {
		due = now
	}
	if due.After(now) {
		return due
	} // A manual run does not postpone a future job.
	if c.Frequency.Unit != "months" {
		return c.Frequency.Next(now, c.AnchorDay)
	}
	due = due.Truncate(time.Minute)
	for i := 0; i < 1200 && !due.After(now); i++ {
		due = c.Frequency.Next(due, c.AnchorDay)
	}
	return due
}
