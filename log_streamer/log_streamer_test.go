package log_streamer_test

import (
	"fmt"
	"strings"

	. "github.com/cloudfoundry-incubator/executor/log_streamer"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/loggregatorlib/logmessage"
)

var _ = Describe("LogStreamer", func() {
	var loggregatorEmitter *FakeLoggregatorEmitter
	var streamer LogStreamer
	guid := "the-guid"

	BeforeEach(func() {
		loggregatorEmitter = NewFakeLoggregatorEmmitter()
		streamer = New(guid, loggregatorEmitter)
	})

	Context("when told to emit", func() {
		Context("when given a message that corresponds to one line", func() {
			BeforeEach(func() {
				fmt.Fprintln(streamer.Stdout(), "this is a log")
				fmt.Fprintln(streamer.Stdout(), "this is another log")
			})

			It("should emit that message", func() {
				Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(2))

				emission := loggregatorEmitter.OutEmissions[0]
				Ω(emission.AppID).Should(Equal(guid))
				Ω(emission.Message).Should(Equal("this is a log"))

				emission = loggregatorEmitter.OutEmissions[1]
				Ω(emission.AppID).Should(Equal(guid))
				Ω(emission.Message).Should(Equal("this is another log"))
			})
		})

		Context("when given a message with all sorts of fun newline characters", func() {
			BeforeEach(func() {
				fmt.Fprintf(streamer.Stdout(), "A\nB\rC\n\rD\r\nE\n\n\nF\r\r\rG\n\r\r\n\n\n\r")
			})

			It("should do the right thing", func() {
				Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(7))
				for i, expectedString := range []string{"A", "B", "C", "D", "E", "F", "G"} {
					Ω(loggregatorEmitter.OutEmissions[i].Message).Should(Equal(expectedString))
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
				Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(1))

				emission := loggregatorEmitter.OutEmissions[0]
				Ω(emission.Message).Should(Equal("this is a log it is made of wood - and it is longerthan it seems"))
			})
		})

		Context("when given a message with multiple new lines", func() {
			BeforeEach(func() {
				fmt.Fprintf(streamer.Stdout(), "this is a log\nand this is another\nand this one isn't done yet...")
			})

			It("should break the message up into multiple loggings", func() {
				Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(2))

				emission := loggregatorEmitter.OutEmissions[0]
				Ω(emission.Message).Should(Equal("this is a log"))

				emission = loggregatorEmitter.OutEmissions[1]
				Ω(emission.Message).Should(Equal("and this is another"))
			})
		})

		Describe("message limits", func() {
			var message string
			Context("when the message is just at the emittable length", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE)
					Ω([]byte(message)).Should(HaveLen(MAX_MESSAGE_SIZE), "Ensure that the byte representation of our message is under the limit")

					fmt.Fprintf(streamer.Stdout(), message)
				})

				It("should break the message up and send multiple messages", func() {
					Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(1))
					emission := loggregatorEmitter.OutEmissions[0]
					Ω(emission.Message).Should(Equal(message))
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
					Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(4))
					Ω(loggregatorEmitter.OutEmissions[0].Message).Should(Equal(strings.Repeat("7", MAX_MESSAGE_SIZE)))
					Ω(loggregatorEmitter.OutEmissions[1].Message).Should(Equal(strings.Repeat("8", MAX_MESSAGE_SIZE)))
					Ω(loggregatorEmitter.OutEmissions[2].Message).Should(Equal(strings.Repeat("9", MAX_MESSAGE_SIZE)))
					Ω(loggregatorEmitter.OutEmissions[3].Message).Should(Equal("hello"))
				})
			})

			Context("when having to deal with byte boundaries", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE-1)
					message += "\u0623\n"
					fmt.Fprintf(streamer.Stdout(), message)
				})

				It("should break the message up and send multiple messages", func() {
					Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(2))
					Ω(loggregatorEmitter.OutEmissions[0].Message).Should(Equal(strings.Repeat("7", MAX_MESSAGE_SIZE-1)))
					Ω(loggregatorEmitter.OutEmissions[1].Message).Should(Equal("\u0623"))
				})
			})

			Context("while concatenating, if the message exceeds the emittable length", func() {
				BeforeEach(func() {
					message = strings.Repeat("7", MAX_MESSAGE_SIZE-2)
					fmt.Fprintf(streamer.Stdout(), message)
					fmt.Fprintf(streamer.Stdout(), "778888\n")
				})

				It("should break the message up and send multiple messages", func() {
					Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(2))
					Ω(loggregatorEmitter.OutEmissions[0].Message).Should(Equal(strings.Repeat("7", MAX_MESSAGE_SIZE)))
					Ω(loggregatorEmitter.OutEmissions[1].Message).Should(Equal("8888"))
				})
			})
		})
	})

	Context("when told to emit stderr", func() {
		It("should handle short messages", func() {
			fmt.Fprintf(streamer.Stderr(), "this is a log\nand this is another\nand this one isn't done yet...")
			Ω(loggregatorEmitter.ErrEmissions).Should(HaveLen(2))

			emission := loggregatorEmitter.ErrEmissions[0]
			Ω(emission.Message).Should(Equal("this is a log"))

			emission = loggregatorEmitter.ErrEmissions[1]
			Ω(emission.Message).Should(Equal("and this is another"))
		})

		It("should handle long messages", func() {
			fmt.Fprintf(streamer.Stderr(), strings.Repeat("e", MAX_MESSAGE_SIZE+1)+"\n")
			Ω(loggregatorEmitter.ErrEmissions).Should(HaveLen(2))

			emission := loggregatorEmitter.ErrEmissions[0]
			Ω(emission.Message).Should(Equal(strings.Repeat("e", MAX_MESSAGE_SIZE)))

			emission = loggregatorEmitter.ErrEmissions[1]
			Ω(emission.Message).Should(Equal("e"))
		})

	})

	Context("when told to flush", func() {
		It("should send whatever log is left in its buffer", func() {
			fmt.Fprintf(streamer.Stdout(), "this is a stdout")
			fmt.Fprintf(streamer.Stderr(), "this is a stderr")

			Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(0))
			Ω(loggregatorEmitter.ErrEmissions).Should(HaveLen(0))

			streamer.Flush()

			Ω(loggregatorEmitter.OutEmissions).Should(HaveLen(1))
			Ω(loggregatorEmitter.ErrEmissions).Should(HaveLen(1))
		})
	})
})

type LogEmission struct {
	AppID   string
	Message string
}

type FakeLoggregatorEmitter struct {
	OutEmissions []LogEmission
	ErrEmissions []LogEmission
}

func NewFakeLoggregatorEmmitter() *FakeLoggregatorEmitter {
	return &FakeLoggregatorEmitter{}
}

func (e *FakeLoggregatorEmitter) Emit(appid, message string) {
	e.OutEmissions = append(e.OutEmissions, LogEmission{
		AppID:   appid,
		Message: message,
	})
}

func (e *FakeLoggregatorEmitter) EmitError(appid, message string) {
	e.ErrEmissions = append(e.ErrEmissions, LogEmission{
		AppID:   appid,
		Message: message,
	})
}

func (e *FakeLoggregatorEmitter) EmitLogMessage(*logmessage.LogMessage) {
	panic("no no no no")
}
