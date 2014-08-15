package timer

import "time"

type Timer interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	Sleep(d time.Duration)
	Every(d time.Duration) <-chan time.Time
}

type realTimer struct{}

func NewTimer() Timer {
	return &realTimer{}
}

func (t *realTimer) Now() time.Time {
	return time.Now()
}

func (t *realTimer) Sleep(d time.Duration) {
	time.Sleep(d)
}

func (t *realTimer) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (t *realTimer) Every(d time.Duration) <-chan time.Time {
	return time.NewTicker(d).C
}
