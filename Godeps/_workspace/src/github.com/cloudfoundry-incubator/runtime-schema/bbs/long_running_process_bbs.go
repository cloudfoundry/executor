package bbs

import (
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/cloudfoundry/storeadapter"
	"path"
)

const LongRunningProcessSchemaRoot = SchemaRoot + "transitional_lrp"

func transitionalLongRunningProcessSchemaPath(lrp models.TransitionalLongRunningProcess) string {
	return path.Join(LongRunningProcessSchemaRoot, lrp.Guid)
}

func watchForLrpModificationsOnState(store storeadapter.StoreAdapter, state models.TransitionalLRPState) (<-chan models.TransitionalLongRunningProcess, chan<- bool, <-chan error) {
	lrps := make(chan models.TransitionalLongRunningProcess)
	stopOuter := make(chan bool)
	errsOuter := make(chan error)

	events, stopInner, errsInner := store.Watch(LongRunningProcessSchemaRoot)

	go func() {
		defer close(lrps)
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
				lrp, err := models.NewTransitionalLongRunningProcessFromJSON(event.Node.Value)
				if err != nil {
					continue
				}

				if lrp.State == state {
					lrps <- lrp
				}

			case err, ok := <-errsInner:
				if ok {
					errsOuter <- err
				}
				return
			}
		}
	}()

	return lrps, stopOuter, errsOuter
}
