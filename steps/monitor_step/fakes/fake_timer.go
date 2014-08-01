package fakes

import (
	"sync"
	"time"
)

type FakeTimer struct {
	sync.RWMutex
	startTime     time.Time
	elapsedTime   time.Duration
	afterRequests []afterRequest
}

func NewFakeTimer(t time.Time) *FakeTimer {
	return &FakeTimer{
		startTime:     t,
		elapsedTime:   0,
		afterRequests: []afterRequest{},
	}
}

func (t *FakeTimer) After(d time.Duration) <-chan time.Time {
	t.Lock()
	defer t.Unlock()
	result := make(chan time.Time)
	t.afterRequests = append(t.afterRequests, afterRequest{
		completionTime: t.Now().Add(d),
		channel:        result,
	})
	return result
}

func (t *FakeTimer) Now() time.Time {
	return t.startTime.Add(t.elapsedTime)
}

func (t *FakeTimer) ActiveAfterCount() int {
	t.RLock()
	defer t.RUnlock()
	return len(t.afterRequests)
}

func (t *FakeTimer) Elapse(d time.Duration) {
	t.elapsedTime += d
	currentTime := t.Now()

	remainingReqs := []afterRequest{}
	for _, req := range t.afterRequests {
		if !req.completionTime.After(currentTime) {
			req.channel <- currentTime
		} else {
			remainingReqs = append(remainingReqs, req)
		}
	}

	t.Lock()
	defer t.Unlock()
	t.afterRequests = remainingReqs
}

type afterRequest struct {
	completionTime time.Time
	channel        chan time.Time
}
