package outbound_test

import (
	"context"
	"sync"
	"testing"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := outbound.NewInMemoryConversationRepository()

	conv, err := model.NewConversation("ext-1")
	require.NoError(t, err)
	require.NoError(t, conv.Open("dlg-1"))

	// Save
	err = repo.Save(ctx, conv)
	require.NoError(t, err)

	// Find by external ID
	found, err := repo.FindByExternalID(ctx, "ext-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "ext-1", found.ExternalID())
	assert.Equal(t, "dlg-1", found.DialogID())

	// Find by dialog ID
	foundByDialog, err := repo.FindByDialogID(ctx, "dlg-1")
	require.NoError(t, err)
	require.NotNil(t, foundByDialog)
	assert.Equal(t, "ext-1", foundByDialog.ExternalID())

	// Find non-existent
	notFound, err := repo.FindByExternalID(ctx, "non-existent")
	assert.ErrorIs(t, err, model.ErrConversationNotFound)
	assert.Nil(t, notFound)

	notFoundDialog, err := repo.FindByDialogID(ctx, "dlg-999")
	assert.ErrorIs(t, err, model.ErrConversationNotFound)
	assert.Nil(t, notFoundDialog)

	// Delete
	err = repo.Delete(ctx, "ext-1")
	require.NoError(t, err)

	_, err = repo.FindByExternalID(ctx, "ext-1")
	assert.ErrorIs(t, err, model.ErrConversationNotFound)
}

func TestInMemoryRepository_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	repo := outbound.NewInMemoryConversationRepository()

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		extID := "ext-user"
		go func() {
			defer wg.Done()
			conv, _ := model.NewConversation(extID)
			_ = conv.Open("dlg-user")
			_ = repo.Save(ctx, conv)
		}()
		go func() {
			defer wg.Done()
			_, _ = repo.FindByExternalID(ctx, extID)
		}()
	}
	wg.Wait()
}
