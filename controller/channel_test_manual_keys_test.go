package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualKeyTestDisablesKeysTheUpstreamRejected(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousCache, previousAutoDisable := common.MemoryCacheEnabled, common.AutomaticDisableChannelEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.AutomaticDisableChannelEnabled = previousCache, previousAutoDisable
	})
	// The global automatic disable stays off: a test started by hand must not
	// depend on it.
	common.MemoryCacheEnabled, common.AutomaticDisableChannelEnabled = false, false

	autoBan := 1
	channel := &model.Channel{
		Type:        constant.ChannelTypeOpenAI,
		Name:        "manual key test",
		Key:         "key-1\nkey-2\nkey-3",
		Status:      common.ChannelStatusEnabled,
		AutoBan:     &autoBan,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 3},
	}
	require.NoError(t, db.Create(channel).Error)
	keys := channel.GetKeys()

	upstream := types.WithOpenAIError(types.OpenAIError{Message: "model not found", Type: "invalid_request_error"}, http.StatusBadRequest)
	transport := types.NewError(errors.New("dial tcp: connection refused"), types.ErrorCodeDoRequestFailed)

	summary := channelTestSummary{}
	assert.True(t, summary.recordManualKeyFailure(channel, keys, 0, nil, upstream, false))
	assert.False(t, summary.recordManualKeyFailure(channel, keys, 1, nil, transport, false), "an unreachable upstream says nothing about the key")
	assert.False(t, summary.recordManualKeyFailure(channel, keys, 2, errors.New("no model to test"), nil, false))
	assert.Equal(t, 1, summary.Disabled)
	assert.Len(t, summary.Errors, 3)
	assert.Contains(t, summary.Errors[0], "#1: ")

	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 1)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)

	// A key that is already disabled is not counted again.
	again := channelTestSummary{}
	assert.False(t, again.recordManualKeyFailure(stored, keys, 0, nil, upstream, false))
	assert.Zero(t, again.Disabled)

	// A channel with automatic disabling turned off keeps its keys and says so.
	autoBan = 0
	stored.AutoBan = &autoBan
	kept := channelTestSummary{}
	assert.False(t, kept.recordManualKeyFailure(stored, keys, 1, nil, upstream, false))
	assert.True(t, kept.AutoBanOff)
	assert.Zero(t, kept.Disabled)
}
