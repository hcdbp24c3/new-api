package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAITextResponseBaseRespRoundTrip(t *testing.T) {
	raw := []byte(`{"id":"x","object":"chat.completion","base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`)

	var resp OpenAITextResponse
	require.NoError(t, kitutil.Unmarshal(raw, &resp))
	require.NotNil(t, resp.BaseResp)
	assert.Equal(t, 1008, resp.BaseResp.StatusCode)
	assert.Equal(t, "insufficient balance", resp.BaseResp.StatusMsg)

	encoded, err := kitutil.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"base_resp"`)
	assert.Contains(t, string(encoded), `"insufficient balance"`)
}

func TestOpenAITextResponseBaseRespOmittedWhenNil(t *testing.T) {
	raw := []byte(`{"id":"x","object":"chat.completion"}`)

	var resp OpenAITextResponse
	require.NoError(t, kitutil.Unmarshal(raw, &resp))
	require.Nil(t, resp.BaseResp)

	encoded, err := kitutil.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "base_resp")
}

func TestChatCompletionsStreamResponseBaseRespRoundTrip(t *testing.T) {
	raw := []byte(`{"id":"c","object":"chat.completion.chunk","choices":null,"base_resp":{"status_code":1004,"status_msg":"token is unusable"}}`)

	var chunk ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal(raw, &chunk))
	require.NotNil(t, chunk.BaseResp)
	assert.Equal(t, 1004, chunk.BaseResp.StatusCode)
	assert.Equal(t, "token is unusable", chunk.BaseResp.StatusMsg)

	encoded, err := kitutil.Marshal(chunk)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"base_resp"`)
}

func TestChatCompletionsStreamResponseBaseRespOmittedWhenNil(t *testing.T) {
	raw := []byte(`{"id":"c","object":"chat.completion.chunk","choices":[]}`)

	var chunk ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal(raw, &chunk))
	require.Nil(t, chunk.BaseResp)

	encoded, err := kitutil.Marshal(chunk)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "base_resp")
}

func TestChatCompletionsStreamResponseCopyDeepCopiesBaseResp(t *testing.T) {
	source := &ChatCompletionsStreamResponse{
		Id:       "c",
		BaseResp: &BaseResp{StatusCode: 1008, StatusMsg: "insufficient balance"},
	}

	copied := source.Copy()
	require.NotNil(t, copied.BaseResp)
	assert.Equal(t, 1008, copied.BaseResp.StatusCode)
	assert.Equal(t, "insufficient balance", copied.BaseResp.StatusMsg)
	assert.NotSame(t, source.BaseResp, copied.BaseResp)

	copied.BaseResp.StatusMsg = "changed"
	assert.Equal(t, "insufficient balance", source.BaseResp.StatusMsg)
}

func TestChatCompletionsStreamResponseCopyKeepsNilBaseRespNil(t *testing.T) {
	source := &ChatCompletionsStreamResponse{Id: "c"}

	copied := source.Copy()
	assert.Nil(t, copied.BaseResp)
}
