package containerstore

import (
	"os"
	"reflect"

	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
)

/*
NewQueuedOrdered starts it's members in order, each member starting when the previous
becomes ready.  On shutdown, it will shut the started processes down in reverse order.
Use an ordered group to describe a list of dependent processes, where each process
depends upon the previous being available in order to function correctly.
*/
func NewQueueOrdered(terminationSignal os.Signal, members grouper.Members) ifrit.Runner {
	return &queueOrdered{
		terminationSignal: terminationSignal,
		pool:              make(map[string]ifrit.Process),
		members:           members,
	}
}

type queueOrdered struct {
	terminationSignal os.Signal
	pool              map[string]ifrit.Process
	members           grouper.Members
}

func (g *queueOrdered) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	err := g.validate()
	if err != nil {
		return err
	}

	signal, errTrace := g.queuedStart(signals)
	if errTrace != nil {
		return g.stop(g.terminationSignal, signals, errTrace)
	}

	if signal != nil {
		return g.stop(signal, signals, errTrace)
	}

	close(ready)

	signal, errTrace = g.waitForSignal(signals, errTrace)
	return g.stop(signal, signals, errTrace)
}

func (g *queueOrdered) validate() error {
	return g.members.Validate()
}

func (g *queueOrdered) queuedStart(signals <-chan os.Signal) (os.Signal, grouper.ErrorTrace) {
	for _, member := range g.members {
		p := ifrit.Background(member)
		cases := make([]reflect.SelectCase, 0, len(g.pool)+3)
		for i := 0; i < len(g.pool); i++ {
			cases = append(cases, reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(g.pool[g.members[i].Name].Wait()),
			})
		}

		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(p.Ready()),
		})

		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(p.Wait()),
		})

		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(signals),
		})

		chosen, recv, _ := reflect.Select(cases)
		g.pool[member.Name] = p
		switch chosen {
		case len(cases) - 1:
			// signals
			return recv.Interface().(os.Signal), nil
		case len(cases) - 2:
			// p.Wait
			var err error
			if !recv.IsNil() {
				err = recv.Interface().(error)
			}
			return nil, grouper.ErrorTrace{
				grouper.ExitEvent{Member: member, Err: err},
			}
		case len(cases) - 3:
			// p.Ready
		default:
			// other member has exited
			var err error = nil
			if e := recv.Interface(); e != nil {
				err = e.(error)
			}
			return nil, grouper.ErrorTrace{
				grouper.ExitEvent{Member: g.members[chosen], Err: err},
			}
		}
	}

	return nil, nil
}

func (g *queueOrdered) waitForSignal(signals <-chan os.Signal, errTrace grouper.ErrorTrace) (os.Signal, grouper.ErrorTrace) {
	cases := make([]reflect.SelectCase, 0, len(g.pool)+1)
	//for i := len(g.pool) - 1; i >= 0; i-- {
	for i := 0; i < len(g.pool); i++ {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(g.pool[g.members[i].Name].Wait()),
		})
	}
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(signals),
	})

	chosen, recv, _ := reflect.Select(cases)
	if chosen == len(cases)-1 {
		return recv.Interface().(os.Signal), errTrace
	}

	var err error
	if !recv.IsNil() {
		err = recv.Interface().(error)
	}

	errTrace = append(errTrace, grouper.ExitEvent{
		Member: g.members[chosen],
		Err:    err,
	})

	return g.terminationSignal, errTrace
}

func (g *queueOrdered) stop(signal os.Signal, signals <-chan os.Signal, errTrace grouper.ErrorTrace) error {
	errOccurred := false
	exited := map[string]struct{}{}
	if len(errTrace) > 0 {
		for _, exitEvent := range errTrace {
			exited[exitEvent.Member.Name] = struct{}{}
			if exitEvent.Err != nil {
				errOccurred = true
			}
		}
	}

	for i := 0; i < len(g.pool); i++ {
		m := g.members[i]
		if _, found := exited[m.Name]; found {
			continue
		}
		if p, ok := g.pool[m.Name]; ok {
			p.Signal(signal)
		Exited:
			for {
				select {
				case err := <-p.Wait():
					errTrace = append(errTrace, grouper.ExitEvent{
						Member: m,
						Err:    err,
					})
					if err != nil {
						errOccurred = true
					}
					break Exited
				case sig := <-signals:
					if sig != signal {
						signal = sig
						p.Signal(signal)
					}
				}
			}
		}
	}

	if errOccurred {
		return errTrace
	}

	return nil
}
