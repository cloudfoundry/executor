package event

import (
	"sync"

	"github.com/cloudfoundry-incubator/executor"
)

const SUBSCRIBER_BUFFER = 1024

//go:generate counterfeiter -o fakes/fake_hub.go . Hub
type Hub interface {
	EmitEvent(executor.Event)
	Subscribe() <-chan executor.Event
	Close()
}

func NewHub() Hub {
	return &hub{}
}

type hub struct {
	subscribers []chan<- executor.Event
	lock        sync.Mutex

	closed bool
}

func (hub *hub) EmitEvent(event executor.Event) {
	hub.lock.Lock()

	remainingSubscribers := []chan<- executor.Event{}

	for _, sub := range hub.subscribers {
		select {
		case sub <- event:
			remainingSubscribers = append(remainingSubscribers, sub)
		default:
			close(sub)
		}
	}

	hub.subscribers = remainingSubscribers

	hub.lock.Unlock()
}

func (hub *hub) Close() {
	hub.lock.Lock()

	for _, sub := range hub.subscribers {
		close(sub)
	}

	hub.subscribers = nil
	hub.closed = true

	hub.lock.Unlock()
}

func (hub *hub) Subscribe() <-chan executor.Event {
	events := make(chan executor.Event, SUBSCRIBER_BUFFER)

	hub.lock.Lock()

	if hub.closed {
		close(events)
	} else {
		hub.subscribers = append(hub.subscribers, events)
	}

	hub.lock.Unlock()

	return events
}
