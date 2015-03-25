package log_streamer_test

import (
	"fmt"
	"strings"
	"sync"

	. "github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/dropsonde/log_sender/fake"
	"github.com/cloudfoundry/dropsonde/logs"
)

var _ = Describe("LogStreamer", func() {
	var fakeSender *fake.FakeLogSender
	var streamer LogStreamer
	guid := "the-guid"
	sourceName := "the-source-name"
	index := 11

	BeforeEach(func() {
		fakeSender = fake.NewFakeLogSender()
		logs.Initialize(fakeSender)

		streamer = New(guid, sourceName, index)
	})

	Context("when told to emit", func() {
		Context("when given a message that corresponds to one line", func() {
			BeforeEach(func() {
				fmt.Fprintln(streamer.Stdout(), "this is a log")
				fmt.Fprintln(streamer.Stdout(), "this is another log")
			})

			It("should emit that message", func() {
				logs := fakeSender.GetLogs()

				Ω(logs).Should(HaveLen(2))

				emission := logs[0]
				Ω(emission.AppId).Should(Equal(guid))
				Ω(emission.SourceType).Should(Equal(sourceName))
				Ω(string(emission.Message)).Should(Equal("this is a log"))
				Ω(emission.MessageType).Should(Equal("OUT"))
				Ω(emission.SourceInstance).Should(Equal("11"))

				emission = logs[1]
				Ω(emission.AppId).Should(Equal(guid))
				Ω(emission.SourceType).Should(Equal(sourceName))
				Ω(emission.SourceInstance).Should(Equal("11"))
				Ω(string(emission.Message)).Should(Equal("this is another log"))
				Ω(emission.MessageType).Should(Equal("OUT"))
			})
		})

		Describe("WithSource", func() {
			Context("when a new log source is provided", func() {
				It("should emit a message with the new log source", func() {
					newSourceName := "new-source-name"
					streamer = streamer.WithSource(newSourceName)
					fmt.Fprintln(streamer.Stdout(), "this is a log")

					logs := fakeSender.GetLogs()
					Ω(logs).Should(HaveLen(1))

					emission := logs[0]
					Ω(emission.SourceType).Should(Equal(newSourceName))
				})
			})

			Context("when no log source is provided", func() {
				It("should emit a message with the existing log source", func() {
					streamer = streamer.WithSource("")
					fmt.Fprintln(streamer.Stdout(), "this is a log")

					logs := fakeSender.GetLogs()
					Ω(logs).Should(HaveLen(1))

					emission := logs[0]
					Ω(emission.SourceType).Should(Equal(sourceName))
				})
			})
		})

		Context("when given a message with all sorts of fun newline characters", func() {
			BeforeEach(func() {
				fmt.Fprintf(streamer.Stdout(), "A\nB\rC\n\rD\r\nE\n\n\nF\r\r\rG\n\r\r\n\n\n\r")
			})

			It("should do the right thing", func() {
				logs := fakeSender.GetLogs()
				Ω(logs).Should(HaveLen(7))
				for i, expectedString := range []string{"A", "B", "C", "D", "E", "F", "G"} {
					Ω(string(logs[i].Message)).Should(Equal(expectedString))
				}
			})
		})

		Context("when given a series of short messages", func() {
			BeforeEach(func() {
				fmt.Fprintf(streamer.Stdout(), "this is a log")
				fmt.Fprintf(streamer.Stdout(), " it is made of wood")
				fmt.Fprintf(streamer.Stdout(), " - and it is longer")
				fmt.Fprintf(streamer.Stdout(), "than it seems\n")
			})

			It("concatenates them, until a new-line is received, and then emits that", func() {
				logs := fakeSender.GetLogs()
				Ω(logs).Should(HaveLen(1))
				emission := fakeSender.GetLogs()[0]
				Ω(string(emission.Message)).Should(Equal("this is a log it is made of wood - and it is longerthan it seems"))
			})
		})

		Context("when given a message with multiple new lines", func() {
			BeforeEach(func() {
				fmt.Fprintf(streamer.Stdout(), "this is a log\nand this is another\nand this one isn't done yet...")
			})

			It("should break the message up into multiple loggings", func() {
				Ω(fakeSender.GetLogs()).Should(HaveLen(2))

				emission := fakeSender.GetLogs()[0]
				Ω(string(emission.Message)).Should(Equal("this is a log"))

				emission = fakeSender.GetLogs()[1]
				Ω(string(emission.Message)).Should(Equal("and this is another"))
			})
		})

		Describe("message limits", func() {
			var message string
			Context("when the message is just at the emittable length", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE)
					Ω([]byte(message)).Should(HaveLen(MAX_MESSAGE_SIZE), "Ensure that the byte representation of our message is under the limit")

					fmt.Fprintf(streamer.Stdout(), message+"\n")
				})

				It("should break the message up and send multiple messages", func() {
					Ω(fakeSender.GetLogs()).Should(HaveLen(1))
					emission := fakeSender.GetLogs()[0]
					Ω(string(emission.Message)).Should(Equal(message))
				})
			})

			Context("when the message exceeds the emittable length", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE)
					message += strings.Repeat("8", MAX_MESSAGE_SIZE)
					message += strings.Repeat("9", MAX_MESSAGE_SIZE)
					message += "hello\n"
					fmt.Fprintf(streamer.Stdout(), message)
				})

				It("should break the message up and send multiple messages", func() {
					Ω(fakeSender.GetLogs()).Should(HaveLen(4))
					Ω(string(fakeSender.GetLogs()[0].Message)).Should(Equal(strings.Repeat("7", MAX_MESSAGE_SIZE)))
					Ω(string(fakeSender.GetLogs()[1].Message)).Should(Equal(strings.Repeat("8", MAX_MESSAGE_SIZE)))
					Ω(string(fakeSender.GetLogs()[2].Message)).Should(Equal(strings.Repeat("9", MAX_MESSAGE_SIZE)))
					Ω(string(fakeSender.GetLogs()[3].Message)).Should(Equal("hello"))
				})
			})

			Context("when having to deal with byte boundaries", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE-1)
					message += "\u0623\n"
					fmt.Fprintf(streamer.Stdout(), message)
				})

				It("should break the message up and send multiple messages", func() {
					Ω(fakeSender.GetLogs()).Should(HaveLen(2))
					Ω(string(fakeSender.GetLogs()[0].Message)).Should(Equal(strings.Repeat("7", MAX_MESSAGE_SIZE-1)))
					Ω(string(fakeSender.GetLogs()[1].Message)).Should(Equal("\u0623"))
				})
			})

			Context("while concatenating, if the message exceeds the emittable length", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE-2)
					fmt.Fprintf(streamer.Stdout(), message)
					fmt.Fprintf(streamer.Stdout(), "778888\n")
				})

				It("should break the message up and send multiple messages", func() {
					Ω(fakeSender.GetLogs()).Should(HaveLen(2))
					Ω(string(fakeSender.GetLogs()[0].Message)).Should(Equal(strings.Repeat("7", MAX_MESSAGE_SIZE)))
					Ω(string(fakeSender.GetLogs()[1].Message)).Should(Equal("8888"))
				})
			})
		})
	})

	Context("when told to emit stderr", func() {
		It("should handle short messages", func() {
			fmt.Fprintf(streamer.Stderr(), "this is a log\nand this is another\nand this one isn't done yet...")
			Ω(fakeSender.GetLogs()).Should(HaveLen(2))

			emission := fakeSender.GetLogs()[0]
			Ω(string(emission.Message)).Should(Equal("this is a log"))
			Ω(emission.SourceType).Should(Equal(sourceName))
			Ω(emission.MessageType).Should(Equal("ERR"))

			emission = fakeSender.GetLogs()[1]
			Ω(string(emission.Message)).Should(Equal("and this is another"))
		})

		It("should handle long messages", func() {
			fmt.Fprintf(streamer.Stderr(), strings.Repeat("e", MAX_MESSAGE_SIZE+1)+"\n")
			Ω(fakeSender.GetLogs()).Should(HaveLen(2))

			emission := fakeSender.GetLogs()[0]
			Ω(string(emission.Message)).Should(Equal(strings.Repeat("e", MAX_MESSAGE_SIZE)))

			emission = fakeSender.GetLogs()[1]
			Ω(string(emission.Message)).Should(Equal("e"))
		})
	})

	Context("when told to flush", func() {
		It("should send whatever log is left in its buffer", func() {
			fmt.Fprintf(streamer.Stdout(), "this is a stdout")
			fmt.Fprintf(streamer.Stderr(), "this is a stderr")

			Ω(fakeSender.GetLogs()).Should(HaveLen(0))

			streamer.Flush()

			Ω(fakeSender.GetLogs()).Should(HaveLen(2))
			Ω(fakeSender.GetLogs()[0].MessageType).Should(Equal("OUT"))
			Ω(fakeSender.GetLogs()[1].MessageType).Should(Equal("ERR"))
		})
	})

	Context("when there is no app guid", func() {
		It("does nothing when told to emit or flush", func() {
			streamer = New("", sourceName, index)

			streamer.Stdout().Write([]byte("hi"))
			streamer.Stderr().Write([]byte("hi"))
			streamer.Flush()

			Ω(fakeSender.GetLogs()).Should(BeEmpty())
		})
	})

	Context("when there is no log source", func() {
		It("defaults to LOG", func() {
			streamer = New(guid, "", -1)

			streamer.Stdout().Write([]byte("hi"))
			streamer.Flush()

			Ω(fakeSender.GetLogs()[0].SourceType).Should(Equal(DefaultLogSource))

		})
	})

	Context("when there is no source index", func() {
		It("defaults to 0", func() {
			streamer = New(guid, sourceName, -1)

			streamer.Stdout().Write([]byte("hi"))
			streamer.Flush()

			Ω(fakeSender.GetLogs()[0].SourceInstance).Should(Equal("-1"))
		})
	})

	Context("with multiple goroutines emitting simultaneously", func() {
		var waitGroup *sync.WaitGroup

		BeforeEach(func() {
			waitGroup = new(sync.WaitGroup)

			for i := 0; i < 2; i++ {
				waitGroup.Add(1)
				go func() {
					defer waitGroup.Done()
					fmt.Fprintln(streamer.Stdout(), "this is a log")
				}()
			}
		})

		AfterEach(func(done Done) {
			defer close(done)
			waitGroup.Wait()
		})

		It("does not trigger data races", func() {
			Eventually(fakeSender.GetLogs).Should(HaveLen(2))
		})
	})
})

type FakeLoggregatorEmitter struct {
	emissions []*events.LogMessage
	sync.Mutex
}

func NewFakeLoggregatorEmmitter() *FakeLoggregatorEmitter {
	return &FakeLoggregatorEmitter{}
}

func (e *FakeLoggregatorEmitter) Emit(appid, message string) {
	panic("no no no no")
}

func (e *FakeLoggregatorEmitter) EmitError(appid, message string) {
	panic("no no no no")
}

func (e *FakeLoggregatorEmitter) EmitLogMessage(msg *events.LogMessage) {
	e.Lock()
	defer e.Unlock()
	e.emissions = append(e.emissions, msg)
}

func (e *FakeLoggregatorEmitter) Emissions() []*events.LogMessage {
	e.Lock()
	defer e.Unlock()
	emissionsCopy := make([]*events.LogMessage, len(e.emissions))
	copy(emissionsCopy, e.emissions)
	return emissionsCopy
}
