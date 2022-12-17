// Code generated by counterfeiter. DO NOT EDIT.
package fake_log_streamer

import (
	"io"
	"sync"

	"code.cloudfoundry.org/executor/depot/log_streamer"
)

type FakeLogStreamer struct {
	FlushStub        func()
	flushMutex       sync.RWMutex
	flushArgsForCall []struct {
	}
	SourceNameStub        func() string
	sourceNameMutex       sync.RWMutex
	sourceNameArgsForCall []struct {
	}
	sourceNameReturns struct {
		result1 string
	}
	sourceNameReturnsOnCall map[int]struct {
		result1 string
	}
	StderrStub        func() io.Writer
	stderrMutex       sync.RWMutex
	stderrArgsForCall []struct {
	}
	stderrReturns struct {
		result1 io.Writer
	}
	stderrReturnsOnCall map[int]struct {
		result1 io.Writer
	}
	StdoutStub        func() io.Writer
	stdoutMutex       sync.RWMutex
	stdoutArgsForCall []struct {
	}
	stdoutReturns struct {
		result1 io.Writer
	}
	stdoutReturnsOnCall map[int]struct {
		result1 io.Writer
	}
	StopStub        func()
	stopMutex       sync.RWMutex
	stopArgsForCall []struct {
	}
	UpdateTagsStub        func(map[string]string)
	updateTagsMutex       sync.RWMutex
	updateTagsArgsForCall []struct {
		arg1 map[string]string
	}
	WithSourceStub        func(string) log_streamer.LogStreamer
	withSourceMutex       sync.RWMutex
	withSourceArgsForCall []struct {
		arg1 string
	}
	withSourceReturns struct {
		result1 log_streamer.LogStreamer
	}
	withSourceReturnsOnCall map[int]struct {
		result1 log_streamer.LogStreamer
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeLogStreamer) Flush() {
	fake.flushMutex.Lock()
	fake.flushArgsForCall = append(fake.flushArgsForCall, struct {
	}{})
	stub := fake.FlushStub
	fake.recordInvocation("Flush", []interface{}{})
	fake.flushMutex.Unlock()
	if stub != nil {
		fake.FlushStub()
	}
}

func (fake *FakeLogStreamer) FlushCallCount() int {
	fake.flushMutex.RLock()
	defer fake.flushMutex.RUnlock()
	return len(fake.flushArgsForCall)
}

func (fake *FakeLogStreamer) FlushCalls(stub func()) {
	fake.flushMutex.Lock()
	defer fake.flushMutex.Unlock()
	fake.FlushStub = stub
}

func (fake *FakeLogStreamer) SourceName() string {
	fake.sourceNameMutex.Lock()
	ret, specificReturn := fake.sourceNameReturnsOnCall[len(fake.sourceNameArgsForCall)]
	fake.sourceNameArgsForCall = append(fake.sourceNameArgsForCall, struct {
	}{})
	stub := fake.SourceNameStub
	fakeReturns := fake.sourceNameReturns
	fake.recordInvocation("SourceName", []interface{}{})
	fake.sourceNameMutex.Unlock()
	if stub != nil {
		return stub()
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeLogStreamer) SourceNameCallCount() int {
	fake.sourceNameMutex.RLock()
	defer fake.sourceNameMutex.RUnlock()
	return len(fake.sourceNameArgsForCall)
}

func (fake *FakeLogStreamer) SourceNameCalls(stub func() string) {
	fake.sourceNameMutex.Lock()
	defer fake.sourceNameMutex.Unlock()
	fake.SourceNameStub = stub
}

func (fake *FakeLogStreamer) SourceNameReturns(result1 string) {
	fake.sourceNameMutex.Lock()
	defer fake.sourceNameMutex.Unlock()
	fake.SourceNameStub = nil
	fake.sourceNameReturns = struct {
		result1 string
	}{result1}
}

func (fake *FakeLogStreamer) SourceNameReturnsOnCall(i int, result1 string) {
	fake.sourceNameMutex.Lock()
	defer fake.sourceNameMutex.Unlock()
	fake.SourceNameStub = nil
	if fake.sourceNameReturnsOnCall == nil {
		fake.sourceNameReturnsOnCall = make(map[int]struct {
			result1 string
		})
	}
	fake.sourceNameReturnsOnCall[i] = struct {
		result1 string
	}{result1}
}

func (fake *FakeLogStreamer) Stderr() io.Writer {
	fake.stderrMutex.Lock()
	ret, specificReturn := fake.stderrReturnsOnCall[len(fake.stderrArgsForCall)]
	fake.stderrArgsForCall = append(fake.stderrArgsForCall, struct {
	}{})
	stub := fake.StderrStub
	fakeReturns := fake.stderrReturns
	fake.recordInvocation("Stderr", []interface{}{})
	fake.stderrMutex.Unlock()
	if stub != nil {
		return stub()
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeLogStreamer) StderrCallCount() int {
	fake.stderrMutex.RLock()
	defer fake.stderrMutex.RUnlock()
	return len(fake.stderrArgsForCall)
}

func (fake *FakeLogStreamer) StderrCalls(stub func() io.Writer) {
	fake.stderrMutex.Lock()
	defer fake.stderrMutex.Unlock()
	fake.StderrStub = stub
}

func (fake *FakeLogStreamer) StderrReturns(result1 io.Writer) {
	fake.stderrMutex.Lock()
	defer fake.stderrMutex.Unlock()
	fake.StderrStub = nil
	fake.stderrReturns = struct {
		result1 io.Writer
	}{result1}
}

func (fake *FakeLogStreamer) StderrReturnsOnCall(i int, result1 io.Writer) {
	fake.stderrMutex.Lock()
	defer fake.stderrMutex.Unlock()
	fake.StderrStub = nil
	if fake.stderrReturnsOnCall == nil {
		fake.stderrReturnsOnCall = make(map[int]struct {
			result1 io.Writer
		})
	}
	fake.stderrReturnsOnCall[i] = struct {
		result1 io.Writer
	}{result1}
}

func (fake *FakeLogStreamer) Stdout() io.Writer {
	fake.stdoutMutex.Lock()
	ret, specificReturn := fake.stdoutReturnsOnCall[len(fake.stdoutArgsForCall)]
	fake.stdoutArgsForCall = append(fake.stdoutArgsForCall, struct {
	}{})
	stub := fake.StdoutStub
	fakeReturns := fake.stdoutReturns
	fake.recordInvocation("Stdout", []interface{}{})
	fake.stdoutMutex.Unlock()
	if stub != nil {
		return stub()
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeLogStreamer) StdoutCallCount() int {
	fake.stdoutMutex.RLock()
	defer fake.stdoutMutex.RUnlock()
	return len(fake.stdoutArgsForCall)
}

func (fake *FakeLogStreamer) StdoutCalls(stub func() io.Writer) {
	fake.stdoutMutex.Lock()
	defer fake.stdoutMutex.Unlock()
	fake.StdoutStub = stub
}

func (fake *FakeLogStreamer) StdoutReturns(result1 io.Writer) {
	fake.stdoutMutex.Lock()
	defer fake.stdoutMutex.Unlock()
	fake.StdoutStub = nil
	fake.stdoutReturns = struct {
		result1 io.Writer
	}{result1}
}

func (fake *FakeLogStreamer) StdoutReturnsOnCall(i int, result1 io.Writer) {
	fake.stdoutMutex.Lock()
	defer fake.stdoutMutex.Unlock()
	fake.StdoutStub = nil
	if fake.stdoutReturnsOnCall == nil {
		fake.stdoutReturnsOnCall = make(map[int]struct {
			result1 io.Writer
		})
	}
	fake.stdoutReturnsOnCall[i] = struct {
		result1 io.Writer
	}{result1}
}

func (fake *FakeLogStreamer) Stop() {
	fake.stopMutex.Lock()
	fake.stopArgsForCall = append(fake.stopArgsForCall, struct {
	}{})
	stub := fake.StopStub
	fake.recordInvocation("Stop", []interface{}{})
	fake.stopMutex.Unlock()
	if stub != nil {
		fake.StopStub()
	}
}

func (fake *FakeLogStreamer) StopCallCount() int {
	fake.stopMutex.RLock()
	defer fake.stopMutex.RUnlock()
	return len(fake.stopArgsForCall)
}

func (fake *FakeLogStreamer) StopCalls(stub func()) {
	fake.stopMutex.Lock()
	defer fake.stopMutex.Unlock()
	fake.StopStub = stub
}

func (fake *FakeLogStreamer) UpdateTags(arg1 map[string]string) {
	fake.updateTagsMutex.Lock()
	fake.updateTagsArgsForCall = append(fake.updateTagsArgsForCall, struct {
		arg1 map[string]string
	}{arg1})
	stub := fake.UpdateTagsStub
	fake.recordInvocation("UpdateTags", []interface{}{arg1})
	fake.updateTagsMutex.Unlock()
	if stub != nil {
		fake.UpdateTagsStub(arg1)
	}
}

func (fake *FakeLogStreamer) UpdateTagsCallCount() int {
	fake.updateTagsMutex.RLock()
	defer fake.updateTagsMutex.RUnlock()
	return len(fake.updateTagsArgsForCall)
}

func (fake *FakeLogStreamer) UpdateTagsCalls(stub func(map[string]string)) {
	fake.updateTagsMutex.Lock()
	defer fake.updateTagsMutex.Unlock()
	fake.UpdateTagsStub = stub
}

func (fake *FakeLogStreamer) UpdateTagsArgsForCall(i int) map[string]string {
	fake.updateTagsMutex.RLock()
	defer fake.updateTagsMutex.RUnlock()
	argsForCall := fake.updateTagsArgsForCall[i]
	return argsForCall.arg1
}

func (fake *FakeLogStreamer) WithSource(arg1 string) log_streamer.LogStreamer {
	fake.withSourceMutex.Lock()
	ret, specificReturn := fake.withSourceReturnsOnCall[len(fake.withSourceArgsForCall)]
	fake.withSourceArgsForCall = append(fake.withSourceArgsForCall, struct {
		arg1 string
	}{arg1})
	stub := fake.WithSourceStub
	fakeReturns := fake.withSourceReturns
	fake.recordInvocation("WithSource", []interface{}{arg1})
	fake.withSourceMutex.Unlock()
	if stub != nil {
		return stub(arg1)
	}
	if specificReturn {
		return ret.result1
	}
	return fakeReturns.result1
}

func (fake *FakeLogStreamer) WithSourceCallCount() int {
	fake.withSourceMutex.RLock()
	defer fake.withSourceMutex.RUnlock()
	return len(fake.withSourceArgsForCall)
}

func (fake *FakeLogStreamer) WithSourceCalls(stub func(string) log_streamer.LogStreamer) {
	fake.withSourceMutex.Lock()
	defer fake.withSourceMutex.Unlock()
	fake.WithSourceStub = stub
}

func (fake *FakeLogStreamer) WithSourceArgsForCall(i int) string {
	fake.withSourceMutex.RLock()
	defer fake.withSourceMutex.RUnlock()
	argsForCall := fake.withSourceArgsForCall[i]
	return argsForCall.arg1
}

func (fake *FakeLogStreamer) WithSourceReturns(result1 log_streamer.LogStreamer) {
	fake.withSourceMutex.Lock()
	defer fake.withSourceMutex.Unlock()
	fake.WithSourceStub = nil
	fake.withSourceReturns = struct {
		result1 log_streamer.LogStreamer
	}{result1}
}

func (fake *FakeLogStreamer) WithSourceReturnsOnCall(i int, result1 log_streamer.LogStreamer) {
	fake.withSourceMutex.Lock()
	defer fake.withSourceMutex.Unlock()
	fake.WithSourceStub = nil
	if fake.withSourceReturnsOnCall == nil {
		fake.withSourceReturnsOnCall = make(map[int]struct {
			result1 log_streamer.LogStreamer
		})
	}
	fake.withSourceReturnsOnCall[i] = struct {
		result1 log_streamer.LogStreamer
	}{result1}
}

func (fake *FakeLogStreamer) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.flushMutex.RLock()
	defer fake.flushMutex.RUnlock()
	fake.sourceNameMutex.RLock()
	defer fake.sourceNameMutex.RUnlock()
	fake.stderrMutex.RLock()
	defer fake.stderrMutex.RUnlock()
	fake.stdoutMutex.RLock()
	defer fake.stdoutMutex.RUnlock()
	fake.stopMutex.RLock()
	defer fake.stopMutex.RUnlock()
	fake.updateTagsMutex.RLock()
	defer fake.updateTagsMutex.RUnlock()
	fake.withSourceMutex.RLock()
	defer fake.withSourceMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeLogStreamer) recordInvocation(key string, args []interface{}) {
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

var _ log_streamer.LogStreamer = new(FakeLogStreamer)
