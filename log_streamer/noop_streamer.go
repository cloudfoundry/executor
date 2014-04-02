package log_streamer

type NoopStreamer struct{}

func (NoopStreamer) StreamStdout(message string) {}
func (NoopStreamer) StreamStderr(message string) {}
func (NoopStreamer) Flush()                      {}
