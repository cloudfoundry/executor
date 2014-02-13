package emitter

import (
	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation"
	"github.com/cloudfoundry/loggregatorlib/logmessage"
	"github.com/cloudfoundry/loggregatorlib/logmessage/testhelpers"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

const SECOND = int64(1 * time.Second)

type MockLoggregatorClient struct {
	received chan *[]byte
}

func (m MockLoggregatorClient) Send(data []byte) {
	m.received <- &data
}

func (m MockLoggregatorClient) Emit() instrumentation.Context {
	return instrumentation.Context{}
}

func TestEmit(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}
	e.Emit("appid", "foo")
	receivedMessage := extractLogMessage(t, <-received)

	assert.Equal(t, receivedMessage.GetMessage(), []byte("foo"))
	assert.Equal(t, receivedMessage.GetAppId(), "appid")
	assert.Equal(t, receivedMessage.GetSourceId(), "42")
	assert.Equal(t, receivedMessage.GetMessageType(), logmessage.LogMessage_OUT)
}

func TestEmitError(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}
	e.EmitError("appid", "foo")
	receivedMessage := extractLogMessage(t, <-received)

	assert.Equal(t, receivedMessage.GetMessage(), []byte("foo"))
	assert.Equal(t, receivedMessage.GetAppId(), "appid")
	assert.Equal(t, receivedMessage.GetSourceId(), "42")
	assert.Equal(t, receivedMessage.GetMessageType(), logmessage.LogMessage_ERR)
}

func TestLogMessageEmit(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}

	logMessage := testhelpers.NewLogMessage("test_msg", "test_app_id")
	logMessage.SourceId = proto.String("src_id")
	e.EmitLogMessage(logMessage)
	receivedMessage := extractLogMessage(t, <-received)

	assert.Equal(t, receivedMessage.GetMessage(), []byte("test_msg"))
	assert.Equal(t, receivedMessage.GetAppId(), "test_app_id")
	assert.Equal(t, receivedMessage.GetSourceId(), "src_id")
}

func TestEmitLogMessageTruncatesLargeMessages(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}

	message := longMessage()
	logMessage := testhelpers.NewLogMessage(message, "test_app_id")

	e.EmitLogMessage(logMessage)

	receivedMessage := extractLogMessage(t, <-received)
	receivedMessageText := receivedMessage.GetMessage()

	truncatedOffset := len(receivedMessageText) - len(TRUNCATED_BYTES)
	expectedBytes := append([]byte(message)[:truncatedOffset], TRUNCATED_BYTES...)

	assert.Equal(t, receivedMessageText, expectedBytes)
	assert.True(t, len(receivedMessageText) >= MAX_MESSAGE_BYTE_SIZE)
}

func TestEmitLogMessageSplitsMessagesOnNewlines(t *testing.T) {
	received := make(chan *[]byte, 10)
	message := "message1\n\rmessage2\nmessage3\r\nmessage4\r"
	logMessage := testhelpers.NewLogMessage(message, "test_app_id")

	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}
	e.EmitLogMessage(logMessage)

	assert.Equal(t, len(received), 4)
}

func TestLogEnvelopeEmitter(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}
	e.Emit("appid", "foo")
	receivedEnvelope := extractLogEnvelope(t, <-received)

	assert.Equal(t, receivedEnvelope.GetLogMessage().GetMessage(), []byte("foo"))
	assert.Equal(t, receivedEnvelope.GetLogMessage().GetAppId(), "appid")
	assert.Equal(t, receivedEnvelope.GetRoutingKey(), "appid")
	assert.Equal(t, receivedEnvelope.GetLogMessage().GetSourceId(), "42")
}

func TestLogEnvelopeValidRoutinKeyInTheEnvelope(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", "secret", nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}
	e.Emit("appid", "foo")
	receivedEnvelope := extractLogEnvelope(t, <-received)

	assert.Equal(t, receivedEnvelope.GetRoutingKey(), "appid")
}

func TestLogEnvelopeSignatureInTheEnvelope(t *testing.T) {
	sharedKey := "shared key"

	received := make(chan *[]byte, 1)
	e, _ := NewEmitter("localhost:3456", "ROUTER", "42", sharedKey, nil)
	e.LoggregatorClient = &MockLoggregatorClient{received}
	e.Emit("appid", "foo")
	receivedEnvelope := extractLogEnvelope(t, <-received)

	assert.True(t, receivedEnvelope.VerifySignature(sharedKey))
}

func TestSourceNameIsSetIfMappingIsUnknown(t *testing.T) {
	received := make(chan *[]byte, 1)
	e, err := NewEmitter("localhost:3456", "RTR", "42", "secret", nil)
	assert.NoError(t, err)
	e.LoggregatorClient = &MockLoggregatorClient{received}

	e.Emit("test_app_id", "test_msg")
	receivedMessage := extractLogMessage(t, <-received)

	assert.Equal(t, receivedMessage.GetSourceName(), "RTR")
}

func TestLogEnvelopeValidSourcetype(t *testing.T) {
	_, err := NewEmitter("localhost:38452", "ROUTER", "42", "secret", nil)
	assert.NoError(t, err)
}

func TestLogEnvelopeEmptyAppIdDoesNotEmit(t *testing.T) {
	emitter, err := NewEmitter("localhost:38452", "ROUTER", "42", "secret", nil)
	assert.NoError(t, err)
	received := make(chan *[]byte, 1)
	emitter.LoggregatorClient = &MockLoggregatorClient{received}

	emitter.Emit("", "foo")
	select {
	case <-received:
		t.Error("This message should not have been emitted since it does not have an AppId")
	default:
		// success
	}

	emitter.Emit("    ", "foo")
	select {
	case <-received:
		t.Error("This message should not have been emitted since it does not have an AppId")
	default:
		// success
	}
}

func extractLogEnvelope(t *testing.T, data *[]byte) *logmessage.LogEnvelope {
	receivedEnvelope := &logmessage.LogEnvelope{}

	err := proto.Unmarshal(*data, receivedEnvelope)

	if err != nil {
		t.Fatalf("Envelope invalid. %s", err)
	}
	return receivedEnvelope
}

func extractLogMessage(t *testing.T, data *[]byte) *logmessage.LogMessage {
	envelope := extractLogEnvelope(t, data)

	return envelope.GetLogMessage()
}

func longMessage() string {
	message := ""
	for i := 0; i < MAX_MESSAGE_BYTE_SIZE*2; i++ {
		message += strconv.Itoa(rand.Int() % 10)
	}
	return message
}
