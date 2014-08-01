package monitor_step

import "time"

type Timer interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realTimer struct{}

func NewTimer() Timer {
	return &realTimer{}
}

func (t *realTimer) Now() time.Time {
	return time.Now()
}

func (t *realTimer) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}
