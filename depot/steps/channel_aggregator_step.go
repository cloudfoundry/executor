package steps

import (
	"sync"
)

type channelAggregatorStep struct {
	sourceChannels     []<-chan struct{}
	destinationChannel chan<- struct{}
	*canceller
}

func NewChannelAggregatorStep(destinationChannel chan<- struct{}, sourceChannels ...<-chan struct{}) *channelAggregatorStep {
	return &channelAggregatorStep{
		sourceChannels:     sourceChannels,
		destinationChannel: destinationChannel,
		canceller:          newCanceller(),
	}
}

func (step *channelAggregatorStep) Perform() error {
	var wg sync.WaitGroup
	wg.Add(2)

	// TODO: range over sourceChannels and make this general
	go func() {
		<-step.sourceChannels[0]
		wg.Done()
	}()
	go func() {
		<-step.sourceChannels[1]
		wg.Done()
	}()

	wg.Wait()
	step.destinationChannel <- struct{}{}

	return nil
}
