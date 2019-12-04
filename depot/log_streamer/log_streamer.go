package log_streamer

import (
	"context"
	"io"
	"strconv"

	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/go-loggregator/rpc/loggregator_v2"
)

const (
	MAX_MESSAGE_SIZE = 61440

	DefaultLogSource = "LOG"
)

//go:generate counterfeiter -o fake_log_streamer/fake_log_streamer.go . LogStreamer
type LogStreamer interface {
	Stdout() io.Writer
	Stderr() io.Writer

	Flush()

	WithSource(sourceName string) LogStreamer
	SourceName() string
	Stop()
}

type logStreamer struct {
	stdout     *streamDestination
	stderr     *streamDestination
	cancelFunc context.CancelFunc
}

func New(guid string, sourceName string, index int, originalTags map[string]string, metronClient loggingclient.IngressClient) LogStreamer {
	if guid == "" {
		return noopStreamer{}
	}

	if sourceName == "" {
		sourceName = DefaultLogSource
	}

	tags := map[string]string{}
	for k, v := range originalTags {
		tags[k] = v
	}

	if _, ok := tags["source_id"]; !ok {
		tags["source_id"] = guid
	}
	sourceIndex := strconv.Itoa(index)
	if _, ok := tags["instance_id"]; !ok {
		tags["instance_id"] = sourceIndex
	}

	ctx, cancelFunc := context.WithCancel(context.Background())

	return &logStreamer{
		stdout: newStreamDestination(
			ctx,
			sourceName,
			tags,
			loggregator_v2.Log_OUT,
			metronClient,
		),

		stderr: newStreamDestination(
			ctx,
			sourceName,
			tags,
			loggregator_v2.Log_ERR,
			metronClient,
		),
		cancelFunc: cancelFunc,
	}
}

func (e *logStreamer) Stdout() io.Writer {
	return e.stdout
}

func (e *logStreamer) Stderr() io.Writer {
	return e.stderr
}

func (e *logStreamer) Flush() {
	e.stdout.lockAndFlush()
	e.stderr.lockAndFlush()
}

func (e *logStreamer) WithSource(sourceName string) LogStreamer {
	if sourceName == "" {
		return e
	}

	return &logStreamer{
		stdout: e.stdout.withSource(sourceName),
		stderr: e.stderr.withSource(sourceName),
	}
}

func (e *logStreamer) SourceName() string {
	return e.stdout.sourceName
}

func (e *logStreamer) Stop() {
	e.cancelFunc()
}
