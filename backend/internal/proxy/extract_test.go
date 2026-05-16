package proxy

import (
	"strings"
	"testing"

	"github.com/airouter/backend/internal/models"
)

// 80 KB single-line — exceeds bufio.Scanner's default 64 KB buffer.
// Must not silently drop the message_delta usage event.
func TestExtractTokens_SSE_LongLineDoesNotLoseUsage(t *testing.T) {
	huge := strings.Repeat("a", 80*1024)
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":1234,"output_tokens":0}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + huge + `"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`,
		``,
	}, "\n")

	var log models.RequestLog
	extractTokens([]byte(sse), &log)

	if log.PromptTokens != 1234 {
		t.Errorf("prompt_tokens = %d, want 1234", log.PromptTokens)
	}
	if log.CompletionTokens != 42 {
		t.Errorf("completion_tokens = %d, want 42", log.CompletionTokens)
	}
	if log.TotalTokens != 1276 {
		t.Errorf("total_tokens = %d, want 1276", log.TotalTokens)
	}
}

func TestExtractTokens_SSE_AnthropicCumulativeOutputTokens(t *testing.T) {
	// Anthropic emits a single message_delta carrying the cumulative output count.
	sse := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":500,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":150}}

`
	var log models.RequestLog
	extractTokens([]byte(sse), &log)
	if log.PromptTokens != 500 || log.CompletionTokens != 150 {
		t.Errorf("got prompt=%d completion=%d, want 500/150", log.PromptTokens, log.CompletionTokens)
	}
}
