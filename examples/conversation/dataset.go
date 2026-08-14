package main

import (
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

// MessageItem represents a test prompt from the dataset.
type MessageItem struct {
	ID             string `json:"id"`
	Message        string `json:"message"`
	Category       string `json:"category"`
	ExpectedTokens int    `json:"expected_tokens"`
}

// defaultMessages provides a fallback dataset when no external CSV is specified.
var defaultMessages = []MessageItem{
	{
		ID:             "MSG_001",
		Message:        "Quali sono i requisiti e i documenti necessari per richiedere una nuova carta di credito?",
		Category:       "cards",
		ExpectedTokens: 15,
	},
	{
		ID:             "MSG_002",
		Message:        "Come posso verificare lo stato di avanzamento della mia richiesta di finanziamento?",
		Category:       "requests",
		ExpectedTokens: 14,
	},
	{
		ID:             "MSG_003",
		Message:        "Qual è il limite massimo di spesa mensile (plafond) previsto per la mia carta?",
		Category:       "limits",
		ExpectedTokens: 16,
	},
	{
		ID:             "MSG_004",
		Message:        "Come posso bloccare immediatamente la mia carta di pagamento in caso di furto o smarrimento?",
		Category:       "security",
		ExpectedTokens: 18,
	},
	{
		ID:             "MSG_005",
		Message:        "Quali sono le commissioni applicate per i prelievi di contante presso sportelli automatici all'estero?",
		Category:       "fees",
		ExpectedTokens: 17,
	},
	{
		ID:             "MSG_006",
		Message:        "Come posso rateizzare un acquisto effettuato con la mia carta di credito?",
		Category:       "installments",
		ExpectedTokens: 14,
	},
	{
		ID:             "MSG_007",
		Message:        "Come posso modificare il codice PIN della mia carta tramite l'applicazione o l'home banking?",
		Category:       "security",
		ExpectedTokens: 16,
	},
	{
		ID:             "MSG_008",
		Message:        "Dove posso consultare l'estratto conto mensile e la rendicontazione dei movimenti?",
		Category:       "statements",
		ExpectedTokens: 15,
	},
}

// LoadDataset loads test messages from a CSV file (format: id,message,category,expected_tokens).
// If the path is empty or the file does not exist, it falls back to the default built-in dataset.
func LoadDataset(filePath string) ([]MessageItem, error) {
	if strings.TrimSpace(filePath) == "" {
		return defaultMessages, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		// Fall back to default messages if file cannot be opened
		return defaultMessages, nil
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV dataset: %w", err)
	}

	if len(records) <= 1 {
		return defaultMessages, nil
	}

	var items []MessageItem
	// Skip header row
	for _, row := range records[1:] {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}

		item := MessageItem{
			ID:             strings.TrimSpace(row[0]),
			Category:       "general",
			ExpectedTokens: 15,
		}

		if len(row) > 1 {
			item.Message = strings.Trim(strings.TrimSpace(row[1]), "\"")
		}
		if len(row) > 2 && strings.TrimSpace(row[2]) != "" {
			item.Category = strings.TrimSpace(row[2])
		}
		if len(row) > 3 {
			if tokens, err := strconv.Atoi(strings.TrimSpace(row[3])); err == nil && tokens > 0 {
				item.ExpectedTokens = tokens
			}
		}

		if item.Message != "" {
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		return defaultMessages, nil
	}

	return items, nil
}

// RandomMessage selects a random MessageItem from the given dataset.
func RandomMessage(items []MessageItem) MessageItem {
	if len(items) == 0 {
		return defaultMessages[0]
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return items[r.Intn(len(items))]
}
