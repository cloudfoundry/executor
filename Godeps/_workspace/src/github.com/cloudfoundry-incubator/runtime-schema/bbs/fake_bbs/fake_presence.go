package fake_bbs

import (
	"errors"
	"time"
)

type FakePresence struct {
	MaintainStatus bool
	Maintained     bool
	Removed        bool
	ticker         *time.Ticker
	done           chan struct{}
}

func (p *FakePresence) Maintain(interval time.Duration) (<-chan bool, error) {
	if p.Maintained == true {
		return nil, errors.New("Already being maintained")
	}

	status := make(chan bool)
	p.done = make(chan struct{})
	go func() {
		status <- p.MaintainStatus
		for {
			select {
			case <-time.Tick(interval):
				status <- p.MaintainStatus
			case <-p.done:
				close(status)
				return
			}
		}
	}()

	p.Maintained = true

	return status, nil
}

func (p *FakePresence) Remove() {
	close(p.done)

	p.Removed = true
}
