package shared

import (
	"reflect"
	"time"

	"github.com/cloudfoundry/storeadapter"
)

func RetryIndefinitelyOnStoreTimeout(callback func() error) error {
	for {
		err := callback()

		if err == storeadapter.ErrorTimeout {
			time.Sleep(time.Second)
			continue
		}

		return err
	}
}

func WatchWithFilter(store storeadapter.StoreAdapter, path string, outChan interface{}, filter interface{}) (chan<- bool, <-chan error) {
	filterType := reflect.TypeOf(filter)
	if filterType.Kind() != reflect.Func {
		panic("filter must be a func")
	}
	if filterType.NumIn() != 1 {
		panic("filter must take one argument")
	}
	if !filterType.In(0).AssignableTo(reflect.TypeOf(storeadapter.WatchEvent{})) {
		panic("filter must take a single WatchEvent argument")
	}
	if filterType.NumOut() != 2 {
		panic("filter must return two arguments")
	}
	if filterType.Out(1).Kind() != reflect.Bool {
		panic("filter must return a bool as its second argument")
	}

	chanType := reflect.TypeOf(outChan)
	if chanType.Kind() != reflect.Chan {
		panic("outChan must be a channel")
	}
	if chanType.ChanDir() != reflect.BothDir {
		panic("outChan should be bidirectional")
	}
	if !filterType.Out(0).AssignableTo(chanType.Elem()) {
		panic("filter must return an object, as its first argument, that can be passed into outChan")
	}

	filterValue := reflect.ValueOf(filter)
	chanValue := reflect.ValueOf(outChan)

	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(path)

	go func() {
		defer chanValue.Close()
		defer close(errsOuter)

		for {
			select {
			case <-stopOuter:
				close(stopInner)
				return

			case event, ok := <-events:
				if !ok {
					return
				}

				out := filterValue.Call([]reflect.Value{reflect.ValueOf(event)})
				filterOK := out[1].Bool()
				if !filterOK {
					continue
				}
				chanValue.Send(out[0])

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
				return
			}
		}
	}()

	return stopOuter, errsOuter
}
