package outbound_test

import (
	"testing"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRecordingData_XML(t *testing.T) {
	xmlData := `
<recording>
  <session>
    <userTurn dateTime="15/08/2026 09:00:00.123">
      <item id="u_u">
        <subItem><value>Hello, I need help with my account</value></subItem>
      </item>
    </userTurn>
    <systemTurn dateTime="15/08/2026 09:00:02.456">
      <item id="u_m">
        <subItem><value>{"answer":"Sure! Please provide your account number.","sources":[{"id":"doc-1","title":"Account Support"}]}</value></subItem>
      </item>
    </systemTurn>
    <userTurn dateTime="15/08/2026 09:01:00.000">
      <item id="u_u">
        <subItem><value>My account is AC-9988</value></subItem>
      </item>
    </userTurn>
    <systemTurn dateTime="15/08/2026 09:01:05.100">
      <item id="u_m">
        <subItem><value>Account AC-9988 is verified and active.</value></subItem>
      </item>
    </systemTurn>
  </session>
</recording>
`

	messages, err := outbound.ParseRecordingData(xmlData)
	require.NoError(t, err)
	require.Len(t, messages, 4)

	// Turn 1: Customer
	assert.Equal(t, model.SenderCustomer, messages[0].Sender())
	assert.Equal(t, "Hello, I need help with my account", messages[0].Content())
	assert.Equal(t, 2026, messages[0].Timestamp().Year())
	assert.Equal(t, time.Month(8), messages[0].Timestamp().Month())
	assert.Equal(t, 15, messages[0].Timestamp().Day())
	assert.Equal(t, 9, messages[0].Timestamp().Hour())

	// Turn 2: Agent with Structured JSON Answer
	assert.Equal(t, model.SenderAgent, messages[1].Sender())
	assert.Equal(t, `{"answer":"Sure! Please provide your account number.","sources":[{"id":"doc-1","title":"Account Support"}]}`, messages[1].Content())
	require.NotNil(t, messages[1].Answer())
	assert.Equal(t, "Sure! Please provide your account number.", messages[1].Answer().Text())
	require.Len(t, messages[1].Answer().Sources(), 1)
	assert.Equal(t, "doc-1", messages[1].Answer().Sources()[0].ID())
	assert.Equal(t, "Account Support", messages[1].Answer().Sources()[0].Title())

	// Turn 3: Customer
	assert.Equal(t, model.SenderCustomer, messages[2].Sender())
	assert.Equal(t, "My account is AC-9988", messages[2].Content())

	// Turn 4: Agent with Plain text answer
	assert.Equal(t, model.SenderAgent, messages[3].Sender())
	assert.Equal(t, "Account AC-9988 is verified and active.", messages[3].Content())
	require.NotNil(t, messages[3].Answer())
	assert.Equal(t, "Account AC-9988 is verified and active.", messages[3].Answer().Text())
	assert.Empty(t, messages[3].Answer().Sources())
}

func TestParseRecordingData_EmptyOrInvalidXML(t *testing.T) {
	// Empty XML returns empty messages without error
	msgs, err := outbound.ParseRecordingData("")
	require.NoError(t, err)
	assert.Empty(t, msgs)

	// Invalid XML returns error
	_, err = outbound.ParseRecordingData("<invalid<xml")
	assert.Error(t, err)
}

func TestParseRecordingData_UTF16EncodingHeader(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-16"?>
<recording>
  <session>
    <userTurn dateTime="15/08/2026 09:00:00.000">
      <item id="u_u">
        <subItem><value>Hello in UTF-16</value></subItem>
      </item>
    </userTurn>
    <systemTurn dateTime="15/08/2026 09:00:01.000">
      <item id="u_m">
        <subItem><value>Response in UTF-16</value></subItem>
      </item>
    </systemTurn>
  </session>
</recording>`

	messages, err := outbound.ParseRecordingData(xmlData)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, model.SenderCustomer, messages[0].Sender())
	assert.Equal(t, "Hello in UTF-16", messages[0].Content())
	assert.Equal(t, model.SenderAgent, messages[1].Sender())
	assert.Equal(t, "Response in UTF-16", messages[1].Content())
}

func TestParseRecordingData_VariousEncodings(t *testing.T) {
	encodings := []string{"utf-16", "UTF-16", "ISO-8859-1", "windows-1252", "US-ASCII", "utf-8"}
	for _, enc := range encodings {
		t.Run(enc, func(t *testing.T) {
			xmlData := `<?xml version="1.0" encoding="` + enc + `"?>
<recording>
  <session>
    <userTurn dateTime="15/08/2026 09:00:00.000">
      <item id="u_u">
        <subItem><value>Test ` + enc + `</value></subItem>
      </item>
    </userTurn>
  </session>
</recording>`
			messages, err := outbound.ParseRecordingData(xmlData)
			require.NoError(t, err)
			require.Len(t, messages, 1)
			assert.Equal(t, "Test "+enc, messages[0].Content())
		})
	}
}

func TestParseRecordingData_VariablesFormat(t *testing.T) {
	xmlData := `<interaction>
<userTurn dateTime="16/08/2026 15:26:17.123">
  <variable id="u_u"><value>Como cancelo meu cartão de crédito?</value></variable>
</userTurn>
<systemTurn dateTime="16/08/2026 15:26:32.456">
  <variable id="dialog.chat.answer"><value>Para cancelar seu cartão de crédito, siga os passos...</value></variable>
  <variable id="u_m"><value>{"answer":"Para cancelar seu cartão de crédito, siga os passos...","sources":[{"id":"doc-bradesco","title":"Bradesco Cartoes.pdf"}]}</value></variable>
</systemTurn>
<userTurn dateTime="16/08/2026 15:27:00.000">
  <variable id="u_u"><value>Obrigado!</value></variable>
</userTurn>
<systemTurn dateTime="16/08/2026 15:27:05.000">
  <variable id="u_m"><value>De nada! Posso ajudar em algo mais?</value></variable>
</systemTurn>
</interaction>`

	messages, err := outbound.ParseRecordingData(xmlData)
	require.NoError(t, err)
	require.Len(t, messages, 4)

	assert.Equal(t, model.SenderCustomer, messages[0].Sender())
	assert.Equal(t, "Como cancelo meu cartão de crédito?", messages[0].Content())

	assert.Equal(t, model.SenderAgent, messages[1].Sender())
	require.NotNil(t, messages[1].Answer())
	assert.Equal(t, "Para cancelar seu cartão de crédito, siga os passos...", messages[1].Answer().Text())
	require.Len(t, messages[1].Answer().Sources(), 1)
	assert.Equal(t, "doc-bradesco", messages[1].Answer().Sources()[0].ID())

	assert.Equal(t, model.SenderCustomer, messages[2].Sender())
	assert.Equal(t, "Obrigado!", messages[2].Content())

	assert.Equal(t, model.SenderAgent, messages[3].Sender())
	assert.Equal(t, "De nada! Posso ajudar em algo mais?", messages[3].Content())
}


