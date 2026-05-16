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
	"strings"
)

// Some upstream "unified" routers (e.g. xynera) emit tool calls as plain text
// using a Hermes/ChatML-style envelope inside the assistant's text content:
//
//	[tool_call]{"name":"Read","arguments":{...}}[/tool_call]
//
// Claude Code (and any Anthropic-native client) does not parse this — it just
// renders the literal text. The functions below detect those envelopes in the
// model output and rewrite them on the fly into proper Anthropic tool_use
// blocks / streaming events so MCP and tool calling work transparently.
//
// When the upstream already returns native tool_use blocks the converter is a
// pure pass-through.

// Opening / closing tags we recognise. We accept all common variants:
//
//	[tool_call]{...}[/tool_call]      ← Hermes / xynera most common
//	<tool_call>{...}</tool_call>      ← Hermes XML
//	[tool_call]{...}                  ← unclosed (truncated by max_tokens etc.)
var (
	toolCallOpeners = []string{"[tool_call]", "<tool_call>"}
	toolCallClosers = []string{"[/tool_call]", "</tool_call>"}
)

// toolCallMatch is one parsed envelope inside a piece of text.
type toolCallMatch struct {
	start, end       int             // span in the original text (incl. open/close tags)
	payload          json.RawMessage // the inner {...} JSON
}

// findToolCalls scans `text` for tool-call envelopes using balanced-JSON parsing
// so the matcher works even when the upstream omits the closing tag (e.g. when
// the response is truncated by max_tokens). Returns matches in order.
func findToolCalls(text string) []toolCallMatch {
	var out []toolCallMatch
	i := 0
	for i < len(text) {
		// Find next opener
		startTag := -1
		startIdx := len(text)
		var openerLen int
		for _, op := range toolCallOpeners {
			if k := strings.Index(text[i:], op); k >= 0 && i+k < startIdx {
				startIdx = i + k
				startTag = i + k
				openerLen = len(op)
			}
		}
		if startTag < 0 {
			break
		}
		j := startTag + openerLen
		// Skip whitespace before the JSON object
		for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
			j++
		}
		if j >= len(text) || text[j] != '{' {
			i = startTag + 1
			continue
		}
		// Walk balanced braces, respecting JSON strings/escapes.
		depth, inStr, esc := 0, false, false
		jsonStart := j
		for j < len(text) {
			ch := text[j]
			if esc {
				esc = false
				j++
				continue
			}
			if inStr {
				switch ch {
				case '\\':
					esc = true
				case '"':
					inStr = false
				}
				j++
				continue
			}
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					j++
					goto balanced
				}
			case '"':
				inStr = true
			}
			j++
		}
		// Ran off the end without balancing — give up on this opener.
		i = startTag + 1
		continue
	balanced:
		jsonEnd := j
		// Optional whitespace + optional closing tag.
		end := jsonEnd
		for end < len(text) && (text[end] == ' ' || text[end] == '\t' || text[end] == '\n' || text[end] == '\r') {
			end++
		}
		for _, cl := range toolCallClosers {
			if strings.HasPrefix(text[end:], cl) {
				end += len(cl)
				break
			}
		}
		out = append(out, toolCallMatch{
			start:   startTag,
			end:     end,
			payload: json.RawMessage(text[jsonStart:jsonEnd]),
		})
		i = end
	}
	return out
}

// looksLikeToolCall returns true if `text` contains any opener tag — fast pre-check.
func looksLikeToolCall(text string) bool {
	for _, op := range toolCallOpeners {
		if strings.Contains(text, op) {
			return true
		}
	}
	return false
}

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
		if json.Unmarshal(t.Arguments, &s) == nil &&
			(strings.HasPrefix(strings.TrimSpace(s), "{") || strings.HasPrefix(strings.TrimSpace(s), "[")) {
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
	if !bytes.Contains(body, []byte("[tool_call]")) && !bytes.Contains(body, []byte("<tool_call>")) {
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
		if !looksLikeToolCall(text) {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}

		matches := findToolCalls(text)
		if len(matches) == 0 {
			newBlocks = append(newBlocks, blockRaw)
			continue
		}

		last := 0
		for _, m := range matches {
			if m.start > last {
				if pre := strings.TrimRight(text[last:m.start], " \t\n"); pre != "" {
					tb, _ := json.Marshal(map[string]any{"type": "text", "text": pre})
					newBlocks = append(newBlocks, tb)
				}
			}
			var tc rawToolCall
			if json.Unmarshal(m.payload, &tc) == nil && tc.Name != "" {
				tu, _ := json.Marshal(map[string]any{
					"type":  "tool_use",
					"id":    newToolUseID(),
					"name":  tc.Name,
					"input": tc.input(),
				})
				newBlocks = append(newBlocks, tu)
				foundToolUse = true
			}
			last = m.end
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
// When the upstream already returns native tool_use blocks (no [tool_call]
// envelopes appear), this is a pure pass-through — every event/data line is
// forwarded byte-faithful, exactly once, with the same SSE framing.
//
// Returns the raw bytes that were captured for downstream logging.
func StreamConvertSSE(w io.Writer, flusher http.Flusher, src io.Reader) []byte {
	var captured bytes.Buffer
	conv := &sseConv{w: w, flusher: flusher, captured: &captured}

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		conv.handleLine(scanner.Text())
	}
	return captured.Bytes()
}

type sseConv struct {
	w        io.Writer
	flusher  http.Flusher
	captured *bytes.Buffer

	// `event: ...` line not yet paired with its `data: ...` line.
	pendingEventName string

	// State for the current upstream content block.
	curBlockType string // "text" / "tool_use" / "thinking" / ...
	textBuf      strings.Builder

	// Output index counter (we may insert extra blocks for split text).
	curOurIdx int

	// Set if we converted at least one [tool_call] envelope.
	insertedToolUse bool
}

// ── Low-level writes ─────────────────────────────────────────────────────────

func (c *sseConv) writeRaw(s string) {
	io.WriteString(c.w, s)
	c.captured.WriteString(s)
}

func (c *sseConv) flushOut() {
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

// passthrough writes a raw `event:` (optional) + `data: <payload>\n\n` event,
// preserving the exact SSE framing the upstream sent us.
func (c *sseConv) passthrough(eventName, data string) {
	if eventName != "" {
		c.writeRaw("event: " + eventName + "\n")
	}
	c.writeRaw("data: " + data + "\n\n")
	c.flushOut()
}

// emit writes a JSON-encoded event we synthesised ourselves.
func (c *sseConv) emit(eventName string, payload []byte) {
	if eventName != "" {
		c.writeRaw("event: " + eventName + "\n")
	}
	c.writeRaw("data: ")
	c.w.Write(payload)
	c.captured.Write(payload)
	c.writeRaw("\n\n")
	c.flushOut()
}

// ── Line dispatcher ──────────────────────────────────────────────────────────

func (c *sseConv) handleLine(line string) {
	if strings.HasPrefix(line, "event:") {
		// Remember the event name; we will print it together with its `data:` line.
		c.pendingEventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return
	}
	if !strings.HasPrefix(line, "data:") {
		// Blank/comment line — these terminate events and are emitted by passthrough/emit
		// already, so we drop standalone ones here.
		return
	}

	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	eventName := c.pendingEventName
	c.pendingEventName = ""

	if data == "" || data == "[DONE]" {
		c.passthrough(eventName, data)
		return
	}

	var ev map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &ev) != nil {
		c.passthrough(eventName, data)
		return
	}
	var typ string
	json.Unmarshal(ev["type"], &typ)

	switch typ {
	case "content_block_start":
		c.handleBlockStart(eventName, ev, data)
	case "content_block_delta":
		c.handleBlockDelta(eventName, ev, data)
	case "content_block_stop":
		c.handleBlockStop(eventName, ev, data)
	case "message_delta":
		c.handleMessageDelta(eventName, ev, data)
	default:
		c.passthrough(eventName, data)
	}
}

// ── Per-event handlers ───────────────────────────────────────────────────────

func (c *sseConv) handleBlockStart(eventName string, ev map[string]json.RawMessage, data string) {
	var info struct {
		Index        int `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
		} `json:"content_block"`
	}
	json.Unmarshal([]byte(data), &info)

	c.curBlockType = info.ContentBlock.Type
	c.textBuf.Reset()

	if c.curBlockType == "text" {
		// Defer — text blocks are buffered and re-emitted on content_block_stop
		// (potentially split into text + tool_use + text segments).
		return
	}

	// Non-text block (already-native tool_use, image, thinking, etc.):
	// rewrite only the `index` to our own counter so it stays continuous
	// after any inserted blocks. Re-emit with the original event name.
	c.emitWithIdx(eventName, ev)
}

func (c *sseConv) handleBlockDelta(eventName string, ev map[string]json.RawMessage, data string) {
	if c.curBlockType != "text" {
		c.emitWithIdx(eventName, ev)
		return
	}
	// Text delta: extract the text and buffer it for tool-call detection.
	if dRaw, ok := ev["delta"]; ok {
		var d struct {
			Text string `json:"text"`
		}
		json.Unmarshal(dRaw, &d)
		c.textBuf.WriteString(d.Text)
	}
}

func (c *sseConv) handleBlockStop(eventName string, ev map[string]json.RawMessage, data string) {
	if c.curBlockType != "text" {
		c.emitWithIdx(eventName, ev)
		c.curOurIdx++
		return
	}
	c.flushTextBlock()
}

func (c *sseConv) handleMessageDelta(eventName string, ev map[string]json.RawMessage, data string) {
	if !c.insertedToolUse {
		c.passthrough(eventName, data)
		return
	}
	// We synthesised tool_use blocks; force stop_reason accordingly so the
	// SDK actually invokes the tool.
	dRaw, ok := ev["delta"]
	if !ok {
		c.passthrough(eventName, data)
		return
	}
	var d map[string]json.RawMessage
	if json.Unmarshal(dRaw, &d) != nil {
		c.passthrough(eventName, data)
		return
	}
	d["stop_reason"] = json.RawMessage(`"tool_use"`)
	d["stop_sequence"] = json.RawMessage(`null`)
	newDelta, _ := json.Marshal(d)
	ev["delta"] = newDelta
	out, _ := json.Marshal(ev)
	c.emit(eventName, out)
}

// ── Buffered text block flush ────────────────────────────────────────────────

func (c *sseConv) flushTextBlock() {
	text := c.textBuf.String()
	if !looksLikeToolCall(text) {
		c.emitTextSegment(text)
		return
	}
	matches := findToolCalls(text)
	if len(matches) == 0 {
		c.emitTextSegment(text)
		return
	}

	last := 0
	for _, m := range matches {
		if m.start > last {
			if seg := strings.TrimRight(text[last:m.start], " \t\n"); seg != "" {
				c.emitTextSegment(seg)
			}
		}
		var tc rawToolCall
		if json.Unmarshal(m.payload, &tc) == nil && tc.Name != "" {
			c.emitToolUseSegment(tc)
			c.insertedToolUse = true
		}
		last = m.end
	}
	if last < len(text) {
		if seg := strings.TrimLeft(text[last:], " \t\n"); seg != "" {
			c.emitTextSegment(seg)
		}
	}
}

func (c *sseConv) emitTextSegment(text string) {
	idx := c.curOurIdx
	startEv, _ := json.Marshal(map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	c.emit("content_block_start", startEv)

	deltaEv, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
	c.emit("content_block_delta", deltaEv)

	stopEv, _ := json.Marshal(map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
	c.emit("content_block_stop", stopEv)
	c.curOurIdx++
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
	c.emit("content_block_start", startEv)

	input := tc.input()
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	partial, _ := json.Marshal(string(input))
	deltaEv := []byte(fmt.Sprintf(
		`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
		idx, string(partial)))
	c.emit("content_block_delta", deltaEv)

	stopEv, _ := json.Marshal(map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
	c.emit("content_block_stop", stopEv)
	c.curOurIdx++
}

// emitWithIdx forwards a parsed event after rewriting its `index` field to our
// own counter. Used for non-text blocks (tool_use, image, thinking) so their
// indices stay continuous even after we insert extra synthesised blocks.
func (c *sseConv) emitWithIdx(eventName string, ev map[string]json.RawMessage) {
	idxJSON, _ := json.Marshal(c.curOurIdx)
	ev["index"] = idxJSON
	out, _ := json.Marshal(ev)
	c.emit(eventName, out)
}
