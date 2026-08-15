package model_test

import (
	"testing"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecentActivity(t *testing.T) {
	now := time.Now().UTC()
	act, err := model.NewRecentActivity(101, "ext-user-123", "Support ticket inquiry", now)
	require.NoError(t, err)
	assert.Equal(t, int64(101), act.ID())
	assert.Equal(t, "ext-user-123", act.ExternalID())
	assert.Equal(t, "Support ticket inquiry", act.Title())
	assert.Equal(t, now, act.StartTime())

	// Empty external ID fails
	_, err = model.NewRecentActivity(101, "", "Some title", now)
	assert.ErrorIs(t, err, model.ErrInvalidExternalID)
}
