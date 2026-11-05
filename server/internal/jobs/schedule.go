package jobs

import (
	"fmt"
	"sort"
	"time"
)

// Schedule says when an instance runs by itself. Either Every (an
// interval) or At (times of the day, in the server's local time zone, on
// Days: 0 = Sunday ... 6 = Saturday; no Days = every day).
type Schedule struct {
	// Every is the interval in seconds, at least 60.
	Every int      `json:"every,omitempty"`
	At    []string `json:"at,omitempty"`
	Days  []int    `json:"days,omitempty"`
}

// Validate checks the schedule and returns it normalised (At sorted,
// Days sorted and unique).
func (s *Schedule) Validate() (*Schedule, error) {
	if s == nil {
		return nil, nil
	}
	switch {
	case s.Every != 0 && len(s.At) > 0:
		return nil, fmt.Errorf("A schedule is either an interval (every) or times of the day (at), not both.")
	case s.Every != 0:
		return s.validateInterval()
	case len(s.At) > 0:
		return s.validateClockTimes()
	default:
		return nil, fmt.Errorf("Give an interval (every) or times of the day (at).")
	}
}

func (s *Schedule) validateInterval() (*Schedule, error) {
	if s.Every < MinInterval || s.Every > 366*24*3600 {
		return nil, fmt.Errorf("The interval must be at least %d seconds and at most a year.", MinInterval)
	}
	if len(s.Days) > 0 {
		return nil, fmt.Errorf("days only goes with times of the day (at).")
	}
	return &Schedule{Every: s.Every}, nil
}

func (s *Schedule) validateClockTimes() (*Schedule, error) {
	if len(s.At) > 24 {
		return nil, fmt.Errorf("At most 24 times of day.")
	}
	at, err := uniqueClockTimes(s.At)
	if err != nil {
		return nil, err
	}
	days, err := uniqueDays(s.Days)
	if err != nil {
		return nil, err
	}
	return &Schedule{Every: s.Every, At: at, Days: days}, nil
}

func uniqueClockTimes(times []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, a := range times {
		if _, _, ok := parseClock(a); !ok {
			return nil, fmt.Errorf("%q is not a time of day (use HH:MM, 24 hours).", a)
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out, nil
}

// uniqueDays sorts and dedupes; all seven days is the same as none.
func uniqueDays(days []int) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	for _, d := range days {
		if d < 0 || d > 6 {
			return nil, fmt.Errorf("days are numbers from 0 (Sunday) to 6 (Saturday).")
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Ints(out)
	if len(out) == 7 {
		return nil, nil
	}
	return out, nil
}

func parseClock(s string) (h, m int, ok bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, 0, false
		}
	}
	h, m = int(s[0]-'0')*10+int(s[1]-'0'), int(s[3]-'0')*10+int(s[4]-'0')
	return h, m, h < 24 && m < 60
}

// Next returns the first run time after "after". For an interval it is
// last+Every (last is the previous start, or when the instance was
// created); the caller decides what to do with a time in the past (the
// scheduler runs it once, at once). For times of the day it is the next
// matching minute in after's time zone; zero when there is none.
func (s *Schedule) Next(after, last time.Time) time.Time {
	if s == nil {
		return time.Time{}
	}
	if s.Every > 0 {
		return last.Add(time.Duration(s.Every) * time.Second)
	}
	days := map[int]bool{}
	for _, d := range s.Days {
		days[d] = true
	}
	loc := after.Location()
	for add := 0; add <= 8; add++ {
		day := after.AddDate(0, 0, add)
		if len(days) > 0 && !days[int(day.Weekday())] {
			continue
		}
		for _, a := range s.At { // sorted
			h, m, _ := parseClock(a)
			t := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
			if t.After(after) {
				return t
			}
		}
	}
	return time.Time{}
}

// Describe is a short English description for logs.
func (s *Schedule) Describe() string {
	if s == nil {
		return "on demand"
	}
	if s.Every > 0 {
		return fmt.Sprintf("every %ds", s.Every)
	}
	return fmt.Sprintf("at %v on %v", s.At, s.Days)
}
