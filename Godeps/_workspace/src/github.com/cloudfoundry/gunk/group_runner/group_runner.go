package group_runner

import (
	"fmt"
	"os"

	"github.com/tedsuo/ifrit"
)

type groupRunner struct {
	members []Member
}

type Member struct {
	Name string
	ifrit.Runner
}

type ExitEvent struct {
	Member Member
	Err    error
}

type ExitTrace []ExitEvent

func (trace ExitTrace) ToError() error {
	for _, exit := range trace {
		if exit.Err != nil {
			return trace
		}
	}
	return nil
}

func (m ExitTrace) Error() string {
	return fmt.Sprintf("")
}

func New(members []Member) ifrit.Runner {
	return &groupRunner{
		members: members,
	}
}

func (r *groupRunner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	processes := []ifrit.Process{}
	exitTrace := make(ExitTrace, 0, len(r.members))
	exitEvents := make(chan ExitEvent)
	shutdown := false

	for _, member := range r.members {
		process := ifrit.Envoke(member)
		processes = append(processes, process)

		go func(member Member) {
			err := <-process.Wait()
			exitEvents <- ExitEvent{
				Err:    err,
				Member: member,
			}
		}(member)
	}

	close(ready)

	for {
		select {
		case sig := <-signals:
			shutdown = true
			for _, process := range processes {
				process.Signal(sig)
			}

		case exit := <-exitEvents:
			exitTrace = append(exitTrace, exit)

			if len(exitTrace) == len(r.members) {
				return exitTrace.ToError()
			}

			if shutdown {
				break
			}

			shutdown = true
			for _, process := range processes {
				process.Signal(os.Interrupt)
			}
		}
	}
}
