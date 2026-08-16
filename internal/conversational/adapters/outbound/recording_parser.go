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
		inTurn        bool
		currentTS     time.Time
		currentItemID string
		inItem        bool
		bufText       strings.Builder

		// Per-turn collected text
		userText     string
		botPrimary   string // from u_m
		botSecondary string // from dialog.chat.answer
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
			case "userTurn", "systemTurn", "agentTurn", "botTurn":
				inTurn = true
				currentTS = time.Now().UTC()
				for _, attr := range elem.Attr {
					if attr.Name.Local == "dateTime" || attr.Name.Local == "time" {
						if parsed, ok := parseDateTime(attr.Value); ok {
							currentTS = parsed
						}
					}
				}
				userText = ""
				botPrimary = ""
				botSecondary = ""

			case "item", "variable":
				for _, attr := range elem.Attr {
					if attr.Name.Local == "id" {
						currentItemID = attr.Value
						inItem = true
						bufText.Reset()
					}
				}
			}

		case xml.CharData:
			if inTurn && inItem {
				bufText.Write(elem)
			}

		case xml.EndElement:
			name := elem.Name.Local
			switch name {
			case "item", "variable":
				if inItem {
					text := strings.TrimSpace(bufText.String())
					if text != "" {
						switch currentItemID {
						case "u_u":
							userText = text
						case "u_m":
							botPrimary = text
						case "dialog.chat.answer":
							botSecondary = text
						}
					}
					inItem = false
					currentItemID = ""
					bufText.Reset()
				}

			case "userTurn":
				if inTurn {
					if userText != "" {
						msg := buildDomainMessage(model.SenderCustomer, userText, currentTS)
						messages = append(messages, msg)
					}
					inTurn = false
					inItem = false
					bufText.Reset()
				}

			case "systemTurn", "agentTurn", "botTurn":
				if inTurn {
					chosen := botPrimary
					if chosen == "" {
						chosen = botSecondary
					}
					if chosen != "" {
						msg := buildDomainMessage(model.SenderAgent, chosen, currentTS)
						messages = append(messages, msg)
					}
					inTurn = false
					inItem = false
					bufText.Reset()
				}
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
