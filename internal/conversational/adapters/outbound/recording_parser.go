package outbound

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

type jsonBotPayload struct {
	Answer  string `json:"answer"`
	Sources []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"sources"`
}

// ParseRecordingData parses an AIW recordingData XML string into a slice of domain model.Message objects.
func ParseRecordingData(xmlData string) ([]model.Message, error) {
	trimmed := strings.TrimSpace(xmlData)
	if trimmed == "" {
		return []model.Message{}, nil
	}

	decoder := xml.NewDecoder(strings.NewReader(trimmed))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	var messages []model.Message

	var (
		inTurn      bool
		currentRole model.Sender
		currentTS   time.Time
		inItemID    string
		bufText     strings.Builder
	)

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		switch elem := token.(type) {
		case xml.StartElement:
			name := elem.Name.Local
			switch name {
			case "userTurn", "systemTurn":
				inTurn = true
				if name == "userTurn" {
					currentRole = model.SenderCustomer
				} else {
					currentRole = model.SenderAgent
				}
				currentTS = time.Now().UTC()
				for _, attr := range elem.Attr {
					if attr.Name.Local == "dateTime" {
						if parsed, ok := parseDateTime(attr.Value); ok {
							currentTS = parsed
						}
					}
				}
			case "item":
				for _, attr := range elem.Attr {
					if attr.Name.Local == "id" {
						inItemID = attr.Value
					}
				}
			}

		case xml.CharData:
			if inTurn && (inItemID == "u_u" || inItemID == "u_m") {
				bufText.Write(elem)
			}

		case xml.EndElement:
			name := elem.Name.Local
			switch name {
			case "item":
				if inItemID == "u_u" || inItemID == "u_m" {
					text := strings.TrimSpace(bufText.String())
					if text != "" {
						msg := buildDomainMessage(currentRole, text, currentTS)
						messages = append(messages, msg)
					}
					bufText.Reset()
					inItemID = ""
				}
			case "userTurn", "systemTurn":
				inTurn = false
				inItemID = ""
				bufText.Reset()
			}
		}
	}

	return messages, nil
}

func parseDateTime(val string) (time.Time, bool) {
	layouts := []string{
		"02/01/2006 15:04:05.000",
		"02/01/2006 15:04:05",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, val); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func buildDomainMessage(role model.Sender, rawText string, ts time.Time) model.Message {
	msgID := uuid.New().String()

	if role == model.SenderAgent {
		var botPayload jsonBotPayload
		if err := json.Unmarshal([]byte(rawText), &botPayload); err == nil && (botPayload.Answer != "" || len(botPayload.Sources) > 0) {
			sources := make([]model.Source, 0, len(botPayload.Sources))
			for _, s := range botPayload.Sources {
				sources = append(sources, model.NewSource(s.ID, s.Title))
			}
			answer := model.NewStructuredAnswer(botPayload.Answer, sources)
			return model.NewMessageWithDetails(msgID, role, rawText, ts, nil, &answer)
		}

		answer := model.NewStructuredAnswer(rawText, nil)
		return model.NewMessageWithDetails(msgID, role, rawText, ts, nil, &answer)
	}

	answer := model.NewStructuredAnswer(rawText, nil)
	return model.NewMessageWithDetails(msgID, role, rawText, ts, nil, &answer)
}
