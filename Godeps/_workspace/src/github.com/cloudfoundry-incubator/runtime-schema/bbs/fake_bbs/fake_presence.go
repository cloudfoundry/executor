package fake_bbs

import (
	"errors"
	"time"
)

type FakePresence struct {
	maintainStatus bool
	maintained     bool
	removed        bool
	ticker         *time.Ticker
	done           chan struct{}
}

func (p *FakePresence) Maintain(interval time.Duration) (<-chan bool, error) {
	if p.ticker != nil {
		return nil, errors.New("Already being maintained")
	}

	status := make(chan bool)
	p.done = make(chan struct{})
	p.ticker = time.NewTicker(interval)
	go func() {
		status <- p.maintainStatus
		for {
			select {
			case <-p.ticker.C:
				status <- p.maintainStatus
			case <-p.done:
				close(status)
				return
			}
		}
	}()

	p.maintained = true

	return status, nil
}

func (p *FakePresence) Remove() {
	p.ticker.Stop()
	p.ticker = nil
	close(p.done)

	p.removed = true
}
