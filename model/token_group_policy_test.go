package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenGroupPolicyRoundTripPreservesPriority(t *testing.T) {
	token := Token{}
	require.NoError(t, token.SetGroupPolicy([]string{"default", "premium"}, true))

	policy, configured, err := token.GetGroupPolicy()
	require.NoError(t, err)
	require.True(t, configured)
	assert.Equal(t, 1, policy.Version)
	assert.Equal(t, []string{"default", "premium"}, policy.Groups)
	assert.True(t, policy.AutoGroup)
	assert.Equal(t, "default", token.Group)
}

func TestPrepareGroupPolicyResponseSupportsLegacyAutoToken(t *testing.T) {
	token := Token{Group: "auto"}
	token.PrepareGroupPolicyResponse()

	assert.Empty(t, token.Groups)
	assert.True(t, token.AutoGroup)
}
