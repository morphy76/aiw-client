package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataset_FallbackDefault(t *testing.T) {
	items, err := LoadDataset("")
	require.NoError(t, err)
	assert.NotEmpty(t, items)
	assert.GreaterOrEqual(t, len(items), 5)

	randMsg := RandomMessage(items)
	assert.NotEmpty(t, randMsg.Message)
	assert.NotEmpty(t, randMsg.ID)
}

func TestDataset_LoadCSV(t *testing.T) {
	tempDir := t.TempDir()
	csvFile := filepath.Join(tempDir, "test_messages.csv")
	csvContent := `id,message,category,expected_tokens
MSG_001,"Come posso attivare la carta?",cards,12
MSG_002,"Qual è il saldo disponibile?",balance,10
`
	err := os.WriteFile(csvFile, []byte(csvContent), 0600)
	require.NoError(t, err)

	items, err := LoadDataset(csvFile)
	require.NoError(t, err)
	require.Len(t, items, 2)

	assert.Equal(t, "MSG_001", items[0].ID)
	assert.Equal(t, "Come posso attivare la carta?", items[0].Message)
	assert.Equal(t, "cards", items[0].Category)
	assert.Equal(t, 12, items[0].ExpectedTokens)

	assert.Equal(t, "MSG_002", items[1].ID)
	assert.Equal(t, "Qual è il saldo disponibile?", items[1].Message)
	assert.Equal(t, "balance", items[1].Category)
	assert.Equal(t, 10, items[1].ExpectedTokens)
}

func TestDataset_NonExistentFileFallback(t *testing.T) {
	items, err := LoadDataset("non_existent_file.csv")
	require.NoError(t, err)
	assert.NotEmpty(t, items)
}
