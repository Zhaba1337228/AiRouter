package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Some upstream "unified" routers (e.g. xynera) emit tool calls as plain text
// using a Hermes/ChatML-style envelope inside the assistant's text content:
//
//	[tool_call]{"name":"Read","arguments":{...}}[/tool_call]
//
// Claude Code (and any Anthropic-native client) does not parse this — it just
// renders the literal text. The functions below detect those envelopes in the
// model output and rewrite them into proper Anthropic tool_use blocks /
// streaming events so MCP and tool calling work transparently.

var toolCallRe = regexp.MustCompile(`(?s)\[tool_call\]\s*(\{.*?\})\s*\[/tool_call\]`)

// rawToolCall is the OpenAI-ish payload we expect inside [tool_call]...[/tool_call].
type rawToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	// Some emitters use "input" instead of "arguments"
	Input json.RawMessage `json:"input"`
}

func (t rawToolCall) input() json.RawMessage {
	if len(t.Input) > 0 {
		return t.Input
	}
	if len(t.Arguments) > 0 {
		// arguments may itself be a JSON-encoded string of JSON ("{\"a\":1}")
		var s string
		if json.Unmarshal(t.Arguments, &s) == nil && (strings.HasPrefix(strings.TrimSpace(s), "{") || strings.HasPrefix(strings.TrimSpace(s), "[")) {
			return json.RawMessage(s)
		}
		return t.Arguments
	}
	return json.RawMessage(`{}`)
}

func newToolUseID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "toolu_" + hex.EncodeToString(b)
}

// ── Non-streaming ─────────────────────────────────────────────────────────────

// ConvertResponse rewrites a non-streaming Anthropic /v1/messages response.
// Returns the body unchanged if it doesn't contain text-tagged tool calls.
func ConvertResponse(body []byte) []byte {
	if !bytes.Contains(body, []byte("[tool_call]")) {
		return body
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return body
	}
	contentRaw, ok := resp["content"]
	if !ok {
		return body
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(contentRaw, &blocks); err != nil {
		return body
	}

	newBlocks := make([]json.RawMessage, 0, len(blocks))
	foundToolUse := false

	for _, blockRaw := range blocks {
		var block map[string]json.RawMessage
		if json.Unmarshal(blockRaw, &block) != nil {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}
		var typ string
		if json.Unmarshal(block["type"], &typ) != nil || typ != "text" {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}
		var text string
		if json.Unmarshal(block["text"], &text) != nil {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}
		if !strings.Contains(text, "[tool_call]") {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}

		matches := toolCallRe.FindAllStringSubmatchIndex(text, -1)
		if len(matches) == 0 {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}

		last := 0
		for _, m := range matches {
			if m[0] > last {
				if pre := strings.TrimRight(text[last:m[0]], " \t\n"); pre != "" {
					tb, _ := json.Marshal(map[string]any{"type": "text", "text": pre})
					newBlocks = append(newBlocks, tb)
				}
			}
			var tc rawToolCall
			body := text[m[2]:m[3]]
			if json.Unmarshal([]byte(body), &tc) == nil && tc.Name != "" {
				tu, _ := json.Marshal(map[string]any{
					"type":  "tool_use",
					"id":    newToolUseID(),
					"name":  tc.Name,
					"input": tc.input(),
				})
				newBlocks = append(newBlocks, tu)
				foundToolUse = true
			}
			last = m[1]
		}
		if last < len(text) {
			if post := strings.TrimLeft(text[last:], " \t\n"); post != "" {
				tb, _ := json.Marshal(map[string]any{"type": "text", "text": post})
				newBlocks = append(newBlocks, tb)
			}
		}
	}

	newContent, _ := json.Marshal(newBlocks)
	resp["content"] = newContent
	if foundToolUse {
		resp["stop_reason"] = json.RawMessage(`"tool_use"`)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return body
	}
	return out
}

// ── Streaming ─────────────────────────────────────────────────────────────────

// StreamConvertSSE proxies an SSE stream from `src` to `w`, converting any
// [tool_call]...[/tool_call] sequences inside text_delta events into native
// content_block_start/stop events with type=tool_use.
//
// Each upstream text content_block is fully buffered until its content_block_stop
// arrives, then re-emitted as one or more (text + tool_use + text) blocks.
// Non-text blocks (already-native tool_use, images, etc.) are forwarded as-is.
//
// Returns the raw bytes that were captured for downstream logging.
func StreamConvertSSE(w io.Writer, flusher http.Flusher, src io.Reader) []byte {
	var captured bytes.Buffer
	conv := &sseConv{w: w, flusher: flusher, captured: &captured}

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		conv.handleLine(line)
	}
	conv.flush()
	return captured.Bytes()
}

type sseConv struct {
	w        io.Writer
	flusher  http.Flusher
	captured *bytes.Buffer

	// Per-block buffering state.
	pendingEventName string                     // last "event: ..." line
	curBlockData     []byte                     // raw data of last content_block_start (so we can replay/edit)
	curBlockType     string                     // "text" / "tool_use" / "thinking" / ...
	curUpstreamIdx   int                        // index from upstream
	curOurIdx        int                        // index we emit
	textBuf          strings.Builder            // buffered text deltas inside current text block
	textBlockOpen    bool                       // we've already emitted content_block_start for the CURRENT segment
	insertedToolUse  bool                       // any [tool_call] was converted in this stream
	deltaSidecars    map[string]json.RawMessage // sidecar fields from a text_delta we want to keep on its replacement
}

func (c *sseConv) write(s string) {
	io.WriteString(c.w, s)
}

func (c *sseConv) writeEvent(name string, data []byte) {
	if name != "" {
		c.write("event: " + name + "\n")
	}
	c.write("data: ")
	c.w.Write(data)
	c.write("\n\n")
	if c.flusher != nil {
		c.flusher.Flush()
	}
	c.captured.WriteString("event: " + name + "\ndata: ")
	c.captured.Write(data)
	c.captured.WriteString("\n\n")
}

func (c *sseConv) writeRawLine(line string) {
	c.write(line + "\n")
	c.captured.WriteString(line + "\n")
}

func (c *sseConv) handleLine(line string) {
	if strings.HasPrefix(line, "event:") {
		c.pendingEventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		c.writeRawLine(line)
		return
	}
	if !strings.HasPrefix(line, "data:") {
		// Blank line / comment / unknown — just forward
		c.writeRawLine(line)
		return
	}

	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		c.writeRawLine(line)
		return
	}

	var ev map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &ev) != nil {
		c.writeRawLine(line)
		return
	}
	var typ string
	json.Unmarshal(ev["type"], &typ)

	switch typ {
	case "content_block_start":
		c.handleBlockStart(line, ev)
	case "content_block_delta":
		c.handleBlockDelta(line, ev)
	case "content_block_stop":
		c.handleBlockStop(line, ev)
	case "message_delta":
		c.handleMessageDelta(line, ev)
	default:
		c.writeRawLine(line)
	}
}

func (c *sseConv) handleBlockStart(line string, ev map[string]json.RawMessage) {
	var info struct {
		Index        int `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
		} `json:"content_block"`
	}
	json.Unmarshal([]byte("{"+strings.SplitN(line, "{", 2)[1]), &info) // tolerate

	c.curUpstreamIdx = info.Index
	c.curBlockType = info.ContentBlock.Type
	c.textBuf.Reset()
	c.textBlockOpen = false

	if c.curBlockType == "text" {
		// Defer emission — we may need to split this block.
		// Store the raw start data to re-emit later (with our own index).
		c.curBlockData = []byte(line)
		return
	}

	// Non-text block: re-emit with our own index (which may be offset due to inserted tool_use blocks)
	c.emitWithIdx("content_block_start", ev, c.curOurIdx)
}

func (c *sseConv) handleBlockDelta(line string, ev map[string]json.RawMessage) {
	if c.curBlockType != "text" {
		c.emitWithIdx("content_block_delta", ev, c.curOurIdx)
		return
	}
	// Text delta: buffer the text part, save sidecar fields for fidelity.
	var delta struct {
		Delta map[string]json.RawMessage `json:"delta"`
	}
	if json.Unmarshal([]byte(line[strings.Index(line, "{"):]), &delta) == nil {
		var text string
		json.Unmarshal(delta.Delta["text"], &text)
		c.textBuf.WriteString(text)
	}
}

func (c *sseConv) handleBlockStop(line string, ev map[string]json.RawMessage) {
	if c.curBlockType != "text" {
		c.emitWithIdx("content_block_stop", ev, c.curOurIdx)
		c.curOurIdx++
		return
	}
	// Flush the buffered text block — possibly split by [tool_call] envelopes.
	c.flushTextBlock()
}

func (c *sseConv) handleMessageDelta(line string, ev map[string]json.RawMessage) {
	if !c.insertedToolUse {
		c.writeRawLine(line)
		return
	}
	// Override stop_reason to tool_use so the SDK actually invokes the tool.
	deltaRaw, ok := ev["delta"]
	if !ok {
		c.writeRawLine(line)
		return
	}
	var delta map[string]json.RawMessage
	if json.Unmarshal(deltaRaw, &delta) != nil {
		c.writeRawLine(line)
		return
	}
	delta["stop_reason"] = json.RawMessage(`"tool_use"`)
	delta["stop_sequence"] = json.RawMessage(`null`)
	newDelta, _ := json.Marshal(delta)
	ev["delta"] = newDelta
	out, _ := json.Marshal(ev)
	c.writeEvent("message_delta", out)
}

func (c *sseConv) flushTextBlock() {
	text := c.textBuf.String()
	matches := toolCallRe.FindAllStringSubmatchIndex(text, -1)

	if len(matches) == 0 {
		// No tool calls: emit as single normal text block.
		c.emitTextSegment(text, true)
		return
	}

	last := 0
	for _, m := range matches {
		if m[0] > last {
			seg := strings.TrimRight(text[last:m[0]], " \t\n")
			if seg != "" {
				c.emitTextSegment(seg, true)
			}
		}
		// Tool call body
		var tc rawToolCall
		body := text[m[2]:m[3]]
		if json.Unmarshal([]byte(body), &tc) == nil && tc.Name != "" {
			c.emitToolUseSegment(tc)
			c.insertedToolUse = true
		}
		last = m[1]
	}
	if last < len(text) {
		seg := strings.TrimLeft(text[last:], " \t\n")
		if seg != "" {
			c.emitTextSegment(seg, true)
		}
	}
}

func (c *sseConv) emitTextSegment(text string, closeAfter bool) {
	idx := c.curOurIdx
	startEv, _ := json.Marshal(map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	c.writeEvent("content_block_start", startEv)

	deltaEv, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
	c.writeEvent("content_block_delta", deltaEv)

	if closeAfter {
		stopEv, _ := json.Marshal(map[string]any{
			"type":  "content_block_stop",
			"index": idx,
		})
		c.writeEvent("content_block_stop", stopEv)
		c.curOurIdx++
	}
}

func (c *sseConv) emitToolUseSegment(tc rawToolCall) {
	idx := c.curOurIdx
	id := newToolUseID()

	startEv, _ := json.Marshal(map[string]any{
		"type":  "content_block_start",
		"index": idx,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  tc.Name,
			"input": map[string]any{},
		},
	})
	c.writeEvent("content_block_start", startEv)

	// Send full input as a single input_json_delta.
	input := tc.input()
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	// input_json_delta expects partial_json as a string
	partial, _ := json.Marshal(string(input))
	deltaEv := []byte(fmt.Sprintf(
		`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
		idx, string(partial)))
	c.writeEvent("content_block_delta", deltaEv)

	stopEv, _ := json.Marshal(map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
	c.writeEvent("content_block_stop", stopEv)
	c.curOurIdx++
}

func (c *sseConv) emitWithIdx(eventName string, ev map[string]json.RawMessage, idx int) {
	idxJSON, _ := json.Marshal(idx)
	ev["index"] = idxJSON
	out, _ := json.Marshal(ev)
	c.writeEvent(eventName, out)
}

func (c *sseConv) flush() {
	// Nothing to do: every block is flushed at content_block_stop.
}
