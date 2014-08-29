package fake_timer

import (
	"sync"
	"time"
)

type FakeTimer struct {
	sync.RWMutex
	now           time.Time
	afterRequests []afterRequest
}

func NewFakeTimer(now time.Time) *FakeTimer {
	return &FakeTimer{now: now}
}

func (t *FakeTimer) After(d time.Duration) <-chan time.Time {
	t.Lock()
	defer t.Unlock()

	result := make(chan time.Time, 1)
	t.afterRequests = append(t.afterRequests, afterRequest{
		completionTime: t.now.Add(d),
		channel:        result,
	})
	return result
}

func (t *FakeTimer) Every(d time.Duration) <-chan time.Time {
	result := make(chan time.Time)
	go func() {
		for {
			time := <-t.After(d)
			result <- time
		}
	}()
	return result
}

func (t *FakeTimer) Sleep(d time.Duration) {
	<-t.After(d)
}

func (t *FakeTimer) Now() time.Time {
	t.RLock()
	defer t.RUnlock()

	return t.now
}

func (t *FakeTimer) Elapse(d time.Duration) {
	// yield to other goroutines first
	time.Sleep(10 * time.Millisecond)

	t.Lock()
	defer t.Unlock()

	t.now = t.now.Add(d)

	remainingReqs := []afterRequest{}
	for _, req := range t.afterRequests {
		if !req.completionTime.After(t.now) {
			req.channel <- t.now
		} else {
			remainingReqs = append(remainingReqs, req)
		}
	}

	t.afterRequests = remainingReqs
}

type afterRequest struct {
	completionTime time.Time
	channel        chan time.Time
}
