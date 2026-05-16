package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertResponse_TextTaggedToolCall(t *testing.T) {
	in := []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Sure. [tool_call]{\"name\":\"Read\",\"arguments\":{\"file_path\":\"D:/x.go\"}}[/tool_call] Done."}],"stop_reason":"end_turn"}`)
	out := ConvertResponse(in)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var stopReason string
	json.Unmarshal(resp["stop_reason"], &stopReason)
	if stopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", stopReason)
	}
	var blocks []map[string]json.RawMessage
	json.Unmarshal(resp["content"], &blocks)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks (text/tool_use/text), got %d: %s", len(blocks), out)
	}
	var typ1, typ2, typ3 string
	json.Unmarshal(blocks[0]["type"], &typ1)
	json.Unmarshal(blocks[1]["type"], &typ2)
	json.Unmarshal(blocks[2]["type"], &typ3)
	if typ1 != "text" || typ2 != "tool_use" || typ3 != "text" {
		t.Errorf("block types = %s/%s/%s, want text/tool_use/text", typ1, typ2, typ3)
	}
	var name string
	json.Unmarshal(blocks[1]["name"], &name)
	if name != "Read" {
		t.Errorf("tool name = %q, want Read", name)
	}
	var input map[string]any
	json.Unmarshal(blocks[1]["input"], &input)
	if input["file_path"] != "D:/x.go" {
		t.Errorf("input.file_path = %v, want D:/x.go", input["file_path"])
	}
}

func TestConvertResponse_UnclosedToolCall(t *testing.T) {
	// Upstream truncated/forgot the [/tool_call] closing tag.
	in := []byte(`{"id":"m","content":[{"type":"text","text":"[tool_call]{\"name\":\"Read\",\"arguments\":{\"file_path\":\"D:/x.go\",\"offset\":70,\"limit\":80}}"}],"stop_reason":"end_turn"}`)
	out := ConvertResponse(in)
	var resp map[string]json.RawMessage
	json.Unmarshal(out, &resp)
	var stopReason string
	json.Unmarshal(resp["stop_reason"], &stopReason)
	if stopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", stopReason)
	}
	var blocks []map[string]json.RawMessage
	json.Unmarshal(resp["content"], &blocks)
	var hasToolUse bool
	for _, b := range blocks {
		var typ string
		json.Unmarshal(b["type"], &typ)
		if typ == "tool_use" {
			hasToolUse = true
			var name string
			json.Unmarshal(b["name"], &name)
			if name != "Read" {
				t.Errorf("name = %q, want Read", name)
			}
		}
	}
	if !hasToolUse {
		t.Errorf("expected tool_use block to be synthesised, got: %s", out)
	}
	if bytes.Contains(out, []byte("[tool_call]")) {
		t.Errorf("[tool_call] envelope leaked through: %s", out)
	}
}

func TestConvertResponse_AngleTagToolCall(t *testing.T) {
	in := []byte(`{"content":[{"type":"text","text":"<tool_call>{\"name\":\"Read\",\"arguments\":{\"file_path\":\"x\"}}</tool_call>"}]}`)
	out := ConvertResponse(in)
	if !bytes.Contains(out, []byte(`"type":"tool_use"`)) {
		t.Errorf("angle-tag envelope not converted: %s", out)
	}
}

func TestConvertResponse_NoToolCall(t *testing.T) {
	in := []byte(`{"content":[{"type":"text","text":"hello"}]}`)
	out := ConvertResponse(in)
	if !bytes.Equal(in, out) {
		t.Errorf("body changed unexpectedly: %s", out)
	}
}

func TestStreamConvertSSE_NativeToolUsePassthrough(t *testing.T) {
	// Upstream already returns native tool_use blocks. The converter must be
	// a faithful pass-through: each `event:` line should appear exactly once
	// in the output, paired with its `data:` line.
	src := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m"}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_x","name":"Read","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"x.go\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var out bytes.Buffer
	StreamConvertSSE(&out, nil, strings.NewReader(src))
	got := out.String()

	for _, name := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	} {
		if c := strings.Count(got, name+"\n"); c != 1 {
			t.Errorf("%q appears %d times in output, want 1:\n%s", name, c, got)
		}
	}
	if !strings.Contains(got, `"type":"tool_use"`) {
		t.Errorf("native tool_use block was not passed through:\n%s", got)
	}
}

func TestStreamConvertSSE_TextTaggedToolCall(t *testing.T) {
	src := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m"}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Sure. [tool_call]"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"{\"name\":\"Read\",\"arguments\":{\"file_path\":\"x.go\"}}"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"[/tool_call] done"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var out bytes.Buffer
	StreamConvertSSE(&out, nil, strings.NewReader(src))
	got := out.String()

	if !strings.Contains(got, `"type":"tool_use"`) {
		t.Errorf("expected native tool_use in output, got:\n%s", got)
	}
	if !strings.Contains(got, `"name":"Read"`) {
		t.Errorf("expected tool name Read, got:\n%s", got)
	}
	if !strings.Contains(got, `"input_json_delta"`) {
		t.Errorf("expected input_json_delta, got:\n%s", got)
	}
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Errorf("expected stop_reason rewritten to tool_use, got:\n%s", got)
	}
	if strings.Contains(got, `[tool_call]`) {
		t.Errorf("[tool_call] markup leaked through, got:\n%s", got)
	}
}
