package model_test

import (
	"testing"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessage_CreationAndAccessors(t *testing.T) {
	now := time.Now().UTC()
	msg, err := model.NewMessage("msg-1", model.SenderCustomer, "Hello world", now)
	require.NoError(t, err)
	assert.Equal(t, "msg-1", msg.ID())
	assert.Equal(t, model.SenderCustomer, msg.Sender())
	assert.Equal(t, "Hello world", msg.Content())
	assert.Equal(t, now, msg.Timestamp())
	assert.Empty(t, msg.Attachments())
	assert.Nil(t, msg.Answer())

	// Empty content fails
	_, err = model.NewMessage("msg-2", model.SenderCustomer, "   ", now)
	assert.ErrorIs(t, err, model.ErrInvalidMessage)
}

func TestMessage_WithAttachmentsAndStructuredAnswer(t *testing.T) {
	now := time.Now().UTC()
	attachment := model.NewAttachment("doc.pdf", "ref-123", map[string]string{"toolName": "pdf-reader"})
	assert.Equal(t, "doc.pdf", attachment.Filename())
	assert.Equal(t, "ref-123", attachment.ContentRef())
	assert.Equal(t, "pdf-reader", attachment.Metadata()["toolName"])

	src1 := model.NewSource("src-1", "User Manual")
	src2 := model.NewSource("src-2", "API Docs")
	answer := model.NewStructuredAnswer("Here is the answer", []model.Source{src1, src2})
	assert.Equal(t, "Here is the answer", answer.Text())
	require.Len(t, answer.Sources(), 2)
	assert.Equal(t, "src-1", answer.Sources()[0].ID())

	msg := model.NewMessageWithDetails(
		"msg-rich",
		model.SenderAgent,
		"Here is the answer",
		now,
		[]model.Attachment{attachment},
		&answer,
	)
	assert.Equal(t, "msg-rich", msg.ID())
	assert.Equal(t, model.SenderAgent, msg.Sender())
	assert.Len(t, msg.Attachments(), 1)
	assert.NotNil(t, msg.Answer())
	assert.Equal(t, "Here is the answer", msg.Answer().Text())
}
