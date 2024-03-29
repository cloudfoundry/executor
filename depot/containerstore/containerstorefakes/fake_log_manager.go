// Code generated by counterfeiter. DO NOT EDIT.
package containerstorefakes

import (
	"sync"
	"time"

	diego_logging_client "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/executor/depot/log_streamer"
)

type FakeLogManager struct {
	NewLogStreamerStub        func(executor.LogConfig, diego_logging_client.IngressClient, int, int64, time.Duration) log_streamer.LogStreamer
	newLogStreamerMutex       sync.RWMutex
	newLogStreamerArgsForCall []struct {
		arg1 executor.LogConfig
		arg2 diego_logging_client.IngressClient
		arg3 int
		arg4 int64
		arg5 time.Duration
	}
	newLogStreamerReturns struct {
		result1 log_streamer.LogStreamer
	}
	newLogStreamerReturnsOnCall map[int]struct {
		result1 log_streamer.LogStreamer
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeLogManager) NewLogStreamer(arg1 executor.LogConfig, arg2 diego_logging_client.IngressClient, arg3 int, arg4 int64, arg5 time.Duration) log_streamer.LogStreamer {
	fake.newLogStreamerMutex.Lock()
	ret, specificReturn := fake.newLogStreamerReturnsOnCall[len(fake.newLogStreamerArgsForCall)]
	fake.newLogStreamerArgsForCall = append(fake.newLogStreamerArgsForCall, struct {
		arg1 executor.LogConfig
		arg2 diego_logging_client.IngressClient
		arg3 int
		arg4 int64
		arg5 time.Duration
	}{arg1, arg2, arg3, arg4, arg5})
	stub := fake.NewLogStreamerStub
	fakeReturns := fake.newLogStreamerReturns
	fake.recordInvocation("NewLogStreamer", []interface{}{arg1, arg2, arg3, arg4, arg5})
	fake.newLogStreamerMutex.Unlock()
	if stub != nil {
		return stub(arg1, arg2, arg3, arg4, arg5)
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeLogManager) NewLogStreamerCallCount() int {
	fake.newLogStreamerMutex.RLock()
	defer fake.newLogStreamerMutex.RUnlock()
	return len(fake.newLogStreamerArgsForCall)
}

func (fake *FakeLogManager) NewLogStreamerCalls(stub func(executor.LogConfig, diego_logging_client.IngressClient, int, int64, time.Duration) log_streamer.LogStreamer) {
	fake.newLogStreamerMutex.Lock()
	defer fake.newLogStreamerMutex.Unlock()
	fake.NewLogStreamerStub = stub
}

func (fake *FakeLogManager) NewLogStreamerArgsForCall(i int) (executor.LogConfig, diego_logging_client.IngressClient, int, int64, time.Duration) {
	fake.newLogStreamerMutex.RLock()
	defer fake.newLogStreamerMutex.RUnlock()
	argsForCall := fake.newLogStreamerArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2, argsForCall.arg3, argsForCall.arg4, argsForCall.arg5
}

func (fake *FakeLogManager) NewLogStreamerReturns(result1 log_streamer.LogStreamer) {
	fake.newLogStreamerMutex.Lock()
	defer fake.newLogStreamerMutex.Unlock()
	fake.NewLogStreamerStub = nil
	fake.newLogStreamerReturns = struct {
		result1 log_streamer.LogStreamer
	}{result1}
}

func (fake *FakeLogManager) NewLogStreamerReturnsOnCall(i int, result1 log_streamer.LogStreamer) {
	fake.newLogStreamerMutex.Lock()
	defer fake.newLogStreamerMutex.Unlock()
	fake.NewLogStreamerStub = nil
	if fake.newLogStreamerReturnsOnCall == nil {
		fake.newLogStreamerReturnsOnCall = make(map[int]struct {
			result1 log_streamer.LogStreamer
		})
	}
	fake.newLogStreamerReturnsOnCall[i] = struct {
		result1 log_streamer.LogStreamer
	}{result1}
}

func (fake *FakeLogManager) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.newLogStreamerMutex.RLock()
	defer fake.newLogStreamerMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeLogManager) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ containerstore.LogManager = new(FakeLogManager)
