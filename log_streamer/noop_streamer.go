package log_streamer

import (
	"io"
	"io/ioutil"
)

type NoopStreamer struct{}

func (NoopStreamer) Stdout() io.Writer { return ioutil.Discard }
func (NoopStreamer) Stderr() io.Writer { return ioutil.Discard }
func (NoopStreamer) Flush()            {}
