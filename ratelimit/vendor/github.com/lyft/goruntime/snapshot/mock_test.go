package snapshot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMock_SetEnabled(t *testing.T) {
	m := NewMock().SetEnabled("thing")
	assert.True(t, m.FeatureEnabled("thing", 0))

	m.SetDisabled("thing")
	assert.False(t, m.FeatureEnabled("thing", 0))

	m.Set("other-thing", "value")
	assert.Equal(t, "value", m.Get("other-thing"))
}
