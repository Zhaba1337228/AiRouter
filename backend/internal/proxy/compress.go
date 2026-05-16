package proxy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ── Mode constants ─────────────────────────────────────────────────────────────

type CompressionMode string

const (
	ModeOff        CompressionMode = "off"
	ModeLite       CompressionMode = "lite"
	ModeStandard   CompressionMode = "standard"
	ModeAggressive CompressionMode = "aggressive"
	ModeUltra      CompressionMode = "ultra"
	ModeRTK        CompressionMode = "rtk"
	ModeStacked    CompressionMode = "stacked"
)

func ParseMode(s string) CompressionMode {
	switch CompressionMode(strings.ToLower(s)) {
	case ModeOff, ModeLite, ModeStandard, ModeAggressive, ModeUltra, ModeRTK, ModeStacked:
		return CompressionMode(strings.ToLower(s))
	default:
		return ModeStandard
	}
}

// ── Shared regex ───────────────────────────────────────────────────────────────

var (
	reMultiBlank    = regexp.MustCompile(`\n{3,}`)
	reMultiSpace    = regexp.MustCompile(`[ \t]{2,}`)
	reTrailingSpace = regexp.MustCompile(`[ \t]+\n`)
	reZeroWidth     = regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{FEFF}]`)
	reDecorLine     = regexp.MustCompile(`(?m)^[-=*#~]{4,}\s*$`)
	reAnsi          = regexp.MustCompile(`\x1b\[[0-9;]*[mGKHF]`)

	// AI filler patterns (Standard+)
	fillerPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(certainly|of course|sure thing|absolutely|great question|excellent question)[!.,]?\s*\n?`),
		regexp.MustCompile(`(?i)(i['']d be (happy|glad|delighted) to\s*[.,]?\s*)`),
		regexp.MustCompile(`(?i)(i['']m happy to help[.!]?\s*)`),
		regexp.MustCompile(`(?i)(feel free to (ask|let me know)[^.]*[.!]\s*)`),
		regexp.MustCompile(`(?i)(i hope (this|that) helps[!.]?\s*)`),
		regexp.MustCompile(`(?i)(please (note|be aware) that\s*)`),
		regexp.MustCompile(`(?i)(it['']s worth noting that\s*)`),
		regexp.MustCompile(`(?i)(as (an AI|a language model)[^,]*,\s*)`),
		regexp.MustCompile(`(?i)(let me know if you (have|need)[^.]*[.!]\s*)`),
		regexp.MustCompile(`(?i)(if you have any (more )?(questions|concerns)[^.]*[.!]\s*)`),
		regexp.MustCompile(`(?i)(i understand (that |your )?[^.]{0,40}\.\s*)`),
		regexp.MustCompile(`(?i)(^(thanks|thank you) for (asking|your question|reaching out)[!.]?\s*\n?)`),
		regexp.MustCompile(`(?i)(in (summary|conclusion),?\s*)`),
	}

	// Stopwords for Ultra mode (common English words that rarely add meaning)
	stopwordRe = regexp.MustCompile(`(?i)\b(the|a|an|is|are|was|were|be|been|being|have|has|had|do|does|did|will|would|could|should|may|might|shall|can|need|dare|ought|used|to|of|in|for|on|with|at|by|from|up|about|into|through|during|before|after|above|below|between|out|off|over|under|again|further|then|once|here|there|when|where|why|how|all|both|each|few|more|most|other|some|such|no|nor|not|only|own|same|so|than|too|very|just|now|also)\b `)

	// Test output summary patterns (RTK)
	reTestSummaryPytest = regexp.MustCompile(`(?m)^(PASSED|FAILED|ERROR|=+ .+ =+|.*passed.*failed.*)`)
	reTestSummaryGo     = regexp.MustCompile(`(?m)^(ok |FAIL|---\ (PASS|FAIL))`)
	reTestSummaryJest   = regexp.MustCompile(`(?m)^(Tests?:|Test Suites?:|PASS |FAIL )`)

	// Build log: keep only error/warning lines (RTK)
	reBuildError = regexp.MustCompile(`(?i)(error|warning|fatal|panic|exception)`)

	// Git log line: "abc1234 commit message" (RTK)
	reGitLogLine = regexp.MustCompile(`(?m)^[0-9a-f]{7,40} .+$`)

	// Stack trace frame (RTK)
	reStackFrame = regexp.MustCompile(`(?m)^\s+at .+:\d+`)

	// JSON-like large blobs (RTK)
	reJsonBlob = regexp.MustCompile(`\{[\s\S]{500,}\}`)
)

// ── Entry point ────────────────────────────────────────────────────────────────

// CompressBody applies the given compression mode to the full request body.
// Handles model routing has already happened; this only touches messages + system.
func CompressBody(body []byte, mode CompressionMode) []byte {
	if mode == ModeOff || len(body) == 0 {
		return body
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	changed := false

	// messages array
	if raw, ok := payload["messages"]; ok {
		if compressed, ok := compressMessages(raw, mode); ok {
			payload["messages"] = compressed
			changed = true
		}
	}

	// Anthropic top-level system field
	if raw, ok := payload["system"]; ok {
		if compressed, ok := compressField(raw, mode); ok {
			payload["system"] = compressed
			changed = true
		}
	}

	if !changed {
		return body
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

// ── Messages ───────────────────────────────────────────────────────────────────

type rawMsg struct {
	Role    string             `json:"role"`
	Content json.RawMessage    `json:"content"`
	Extra   map[string]json.RawMessage `json:"-"`
}

func (m rawMsg) MarshalJSON() ([]byte, error) {
	obj := make(map[string]json.RawMessage, len(m.Extra)+2)
	for k, v := range m.Extra {
		obj[k] = v
	}
	obj["role"], _ = json.Marshal(m.Role)
	obj["content"] = m.Content
	return json.Marshal(obj)
}

func parseMessages(raw json.RawMessage) ([]rawMsg, error) {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	msgs := make([]rawMsg, 0, len(arr))
	for _, m := range arr {
		rm := rawMsg{Extra: make(map[string]json.RawMessage)}
		if r, ok := m["role"]; ok {
			json.Unmarshal(r, &rm.Role)
		}
		if c, ok := m["content"]; ok {
			rm.Content = c
		}
		for k, v := range m {
			if k != "role" && k != "content" {
				rm.Extra[k] = v
			}
		}
		msgs = append(msgs, rm)
	}
	return msgs, nil
}

func compressMessages(raw json.RawMessage, mode CompressionMode) (json.RawMessage, bool) {
	msgs, err := parseMessages(raw)
	if err != nil {
		return raw, false
	}

	n := len(msgs)
	changed := false

	for i, m := range msgs {
		// Determine "age" of message (distance from end): 0 = newest
		age := n - 1 - i

		newContent, ok := compressContent(m.Content, mode, age, m.Role)
		if ok {
			msgs[i].Content = newContent
			changed = true
		}
	}

	// Sliding window: if total still too large, drop oldest non-system messages
	if totalContentChars(msgs) > 400_000 {
		msgs = pruneMessages(msgs)
		changed = true
	}

	if !changed {
		return raw, false
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		return raw, false
	}
	return b, true
}

func totalContentChars(msgs []rawMsg) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

// pruneMessages drops the oldest messages while preserving tool_use/tool_result
// pairs. Each assistant message that contains tool_use blocks and its immediately
// following user messages with tool_result blocks form an atomic group that must
// never be split — otherwise the model receives orphaned tool_results and errors.
func pruneMessages(msgs []rawMsg) []rawMsg {
	const keep = 20
	var sys, rest []rawMsg
	for _, m := range msgs {
		if m.Role == "system" {
			sys = append(sys, m)
		} else {
			rest = append(rest, m)
		}
	}
	if len(rest) <= keep {
		return msgs
	}

	// Build atomic groups: tool_use + its tool_result(s) must stay together.
	type group struct{ start, end int } // [start, end) indices into rest
	var groups []group
	i := 0
	for i < len(rest) {
		start := i
		// If this assistant message has tool_use, absorb following tool_result user msgs
		if rest[i].Role == "assistant" && msgHasBlockType(rest[i], "tool_use") {
			for i+1 < len(rest) && rest[i+1].Role == "user" && msgHasBlockType(rest[i+1], "tool_result") {
				i++
			}
		}
		groups = append(groups, group{start, i + 1})
		i++
	}

	// Drop whole groups from the front until we are at/below keep
	dropped := 0
	for len(groups) > 0 && len(rest)-dropped > keep {
		dropped += groups[0].end - groups[0].start
		groups = groups[1:]
	}

	if dropped == 0 {
		return msgs
	}

	notice, _ := json.Marshal(fmt.Sprintf(
		"[%d older messages removed by AiRouter context compression. Continuing from recent context.]",
		dropped,
	))
	ack, _ := json.Marshal("Understood.")
	result := make([]rawMsg, 0, len(sys)+2+len(rest)-dropped)
	result = append(result, sys...)
	result = append(result, rawMsg{Role: "user", Content: notice, Extra: map[string]json.RawMessage{}})
	result = append(result, rawMsg{Role: "assistant", Content: ack, Extra: map[string]json.RawMessage{}})
	result = append(result, rest[dropped:]...)
	return result
}

// msgHasBlockType returns true if the message content is an array that contains
// at least one block with the given "type" field (e.g. "tool_use", "tool_result").
func msgHasBlockType(m rawMsg, blockType string) bool {
	var parts []json.RawMessage
	if json.Unmarshal(m.Content, &parts) != nil {
		return false
	}
	for _, part := range parts {
		var block map[string]json.RawMessage
		if json.Unmarshal(part, &block) != nil {
			continue
		}
		var typ string
		if json.Unmarshal(block["type"], &typ) == nil && typ == blockType {
			return true
		}
	}
	return false
}

// compressField compresses a top-level JSON string field.
func compressField(raw json.RawMessage, mode CompressionMode) (json.RawMessage, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return raw, false
	}
	compressed, ok := compressText(s, mode, 0, "system")
	if !ok {
		return raw, false
	}
	b, _ := json.Marshal(compressed)
	return b, true
}

// ── Content compression (string or array) ─────────────────────────────────────

func compressContent(raw json.RawMessage, mode CompressionMode, age int, role string) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	// String content
	var s string
	if json.Unmarshal(raw, &s) == nil {
		compressed, ok := compressText(s, mode, age, role)
		if !ok {
			return raw, false
		}
		b, _ := json.Marshal(compressed)
		return b, true
	}

	// Array of content blocks (OpenAI vision / Anthropic)
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return raw, false
	}
	changed := false
	for i, part := range parts {
		var block map[string]json.RawMessage
		if json.Unmarshal(part, &block) != nil {
			continue
		}
		var typ string
		if json.Unmarshal(block["type"], &typ) != nil {
			continue
		}

		switch typ {
		case "text":
			var text string
			if json.Unmarshal(block["text"], &text) != nil {
				continue
			}
			compressed, ok := compressText(text, mode, age, role)
			if !ok {
				continue
			}
			block["text"], _ = json.Marshal(compressed)
			parts[i], _ = json.Marshal(block)
			changed = true

		case "tool_result":
			// Compress the text inside tool_result for aggressive/RTK modes.
			// This keeps the block structure intact but shrinks large tool outputs.
			if mode != ModeAggressive && mode != ModeUltra && mode != ModeRTK && mode != ModeStacked {
				continue
			}
			var content string
			if json.Unmarshal(block["content"], &content) != nil {
				// content may itself be an array of blocks — skip for now
				continue
			}
			compressed, ok := compressText(content, mode, age, "tool")
			if !ok {
				continue
			}
			block["content"], _ = json.Marshal(compressed)
			parts[i], _ = json.Marshal(block)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	b, _ := json.Marshal(parts)
	return b, true
}

// ── Per-mode text compression ─────────────────────────────────────────────────

const (
	maxSingleMsg = 48_000 // chars — truncate single message above this
)

func compressText(s string, mode CompressionMode, age int, role string) (string, bool) {
	orig := s

	switch mode {

	case ModeLite:
		s = applyLite(s)

	case ModeStandard:
		s = applyLite(s)
		s = applyStandard(s)
		s = truncateIfNeeded(s, maxSingleMsg)

	case ModeAggressive:
		s = applyLite(s)
		s = applyStandard(s)
		s = applyAggressive(s, age, role)
		s = truncateIfNeeded(s, maxSingleMsg)

	case ModeUltra:
		s = applyLite(s)
		s = applyStandard(s)
		s = applyAggressive(s, age, role)
		s = applyUltra(s, age)
		s = truncateIfNeeded(s, maxSingleMsg/2)

	case ModeRTK:
		s = applyRTK(s)
		s = applyLite(s)
		s = truncateIfNeeded(s, maxSingleMsg)

	case ModeStacked:
		s = applyRTK(s)
		s = applyLite(s)
		s = applyStandard(s)
		s = truncateIfNeeded(s, maxSingleMsg)
	}

	if s == orig {
		return s, false
	}
	return strings.TrimFunc(s, unicode.IsSpace), true
}

// ── Lite: whitespace only ──────────────────────────────────────────────────────

func applyLite(s string) string {
	// Split on code fences to protect code content
	parts := splitCodeFences(s)
	for i, p := range parts {
		if i%2 == 0 { // prose
			p = reZeroWidth.ReplaceAllString(p, "")
			p = reTrailingSpace.ReplaceAllString(p, "\n")
			p = reMultiBlank.ReplaceAllString(p, "\n\n")
			p = reMultiSpace.ReplaceAllString(p, " ")
			parts[i] = p
		}
	}
	return strings.Join(parts, "```")
}

// ── Standard: filler + decorations ────────────────────────────────────────────

func applyStandard(s string) string {
	parts := splitCodeFences(s)
	for i, p := range parts {
		if i%2 == 0 {
			// Remove decorative separator lines
			p = reDecorLine.ReplaceAllString(p, "")
			// Remove filler phrases
			for _, re := range fillerPatterns {
				p = re.ReplaceAllString(p, "")
			}
			// Collapse repeated blank lines created by removals
			p = reMultiBlank.ReplaceAllString(p, "\n\n")
			parts[i] = p
		}
	}
	return strings.Join(parts, "```")
}

// ── Aggressive: age-based truncation + tool result capping ────────────────────

func applyAggressive(s string, age int, role string) string {
	// Cap tool/function results
	if role == "tool" || role == "function" {
		s = capLines(s, 40)
	}

	// Age-based progressive trimming
	if age >= 8 && len(s) > 600 {
		s = s[:500] + fmt.Sprintf("\n[...%d chars condensed by AiRouter...]", len(s)-500)
	}

	return s
}

// ── Ultra: stopwords + extreme aging ──────────────────────────────────────────

func applyUltra(s string, age int) string {
	if age >= 6 && len(s) > 300 {
		s = s[:200] + fmt.Sprintf("\n[...%d chars removed (ultra compression)...]", len(s)-200)
		return s
	}

	// Stopword removal from prose only (rough: apply outside code fences)
	parts := splitCodeFences(s)
	for i, p := range parts {
		if i%2 == 0 {
			parts[i] = stopwordRe.ReplaceAllString(p, " ")
		}
	}
	s = strings.Join(parts, "```")
	s = reMultiSpace.ReplaceAllString(s, " ")
	return s
}

// ── RTK: domain-specific output compression ───────────────────────────────────

func applyRTK(s string) string {
	// Strip ANSI escape codes
	s = reAnsi.ReplaceAllString(s, "")

	// Detect and compress specific output types
	switch {
	case isTestOutput(s):
		s = compressTestOutput(s)
	case isBuildLog(s):
		s = compressBuildLog(s)
	case isGitLog(s):
		s = compressGitLog(s)
	case isStackTrace(s):
		s = compressStackTrace(s)
	case isShellOutput(s):
		s = compressShellOutput(s)
	default:
		// Compact large JSON blobs
		s = reJsonBlob.ReplaceAllStringFunc(s, minifyJSON)
	}

	return s
}

func isTestOutput(s string) bool {
	return reTestSummaryPytest.MatchString(s) ||
		reTestSummaryGo.MatchString(s) ||
		reTestSummaryJest.MatchString(s)
}

func isBuildLog(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) < 5 {
		return false
	}
	infoCount, errCount := 0, 0
	for _, l := range lines {
		if reBuildError.MatchString(l) {
			errCount++
		} else if strings.TrimSpace(l) != "" {
			infoCount++
		}
	}
	return infoCount > 10 && errCount > 0
}

func isGitLog(s string) bool {
	matches := reGitLogLine.FindAllString(s, -1)
	return len(matches) >= 3
}

func isStackTrace(s string) bool {
	return reStackFrame.FindStringIndex(s) != nil && strings.Count(s, "\n    at ") >= 3
}

func isShellOutput(s string) bool {
	lines := strings.Split(s, "\n")
	return len(lines) > 60
}

func compressTestOutput(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if reTestSummaryPytest.MatchString(line) ||
			reTestSummaryGo.MatchString(line) ||
			reTestSummaryJest.MatchString(line) ||
			strings.Contains(strings.ToLower(line), "error") ||
			strings.Contains(strings.ToLower(line), "fail") {
			keep = append(keep, line)
		}
	}
	if len(keep) == 0 {
		return s
	}
	return "[RTK: test summary]\n" + strings.Join(keep, "\n")
}

func compressBuildLog(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if reBuildError.MatchString(line) {
			keep = append(keep, line)
		}
	}
	if len(keep) == 0 {
		return s
	}
	return "[RTK: build errors/warnings only]\n" + strings.Join(keep, "\n")
}

func compressGitLog(s string) string {
	matches := reGitLogLine.FindAllString(s, 30)
	if len(matches) == 0 {
		return s
	}
	total := len(reGitLogLine.FindAllString(s, -1))
	out := "[RTK: git log (first 30)]\n" + strings.Join(matches, "\n")
	if total > 30 {
		out += fmt.Sprintf("\n... (%d more commits)", total-30)
	}
	return out
}

func compressStackTrace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	frameCount := 0
	for _, line := range lines {
		isFrame := strings.Contains(line, "    at ")
		if !isFrame {
			result = append(result, line)
		} else if frameCount < 5 {
			result = append(result, line)
			frameCount++
		} else if frameCount == 5 {
			remaining := 0
			for _, l := range lines {
				if strings.Contains(l, "    at ") {
					remaining++
				}
			}
			result = append(result, fmt.Sprintf("    ... (%d more frames, RTK compressed)", remaining-5))
			frameCount++
		}
	}
	return strings.Join(result, "\n")
}

func compressShellOutput(s string) string {
	lines := strings.Split(s, "\n")
	const maxLines = 80
	if len(lines) <= maxLines {
		return s
	}
	// Deduplicate consecutive identical lines
	deduped := make([]string, 0, len(lines))
	for i, l := range lines {
		if i > 0 && l == lines[i-1] {
			if i > 1 && lines[i-1] == lines[i-2] {
				continue // skip beyond second repeat
			}
		}
		deduped = append(deduped, l)
	}
	if len(deduped) > maxLines {
		skipped := len(deduped) - maxLines
		deduped = append(
			[]string{fmt.Sprintf("[RTK: skipped %d lines]", skipped)},
			deduped[skipped:]...,
		)
	}
	return strings.Join(deduped, "\n")
}

func minifyJSON(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	if len(b) < len(s) {
		return string(b)
	}
	return s
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// splitCodeFences splits text by ``` preserving fence markers as separators.
func splitCodeFences(s string) []string {
	return strings.Split(s, "```")
}

func capLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	head := max * 3 / 4
	tail := max / 4
	skipped := len(lines) - head - tail
	result := append(lines[:head],
		append([]string{fmt.Sprintf("[...%d lines omitted (RTK/Aggressive)...]", skipped)},
			lines[len(lines)-tail:]...)...)
	return strings.Join(result, "\n")
}

func truncateIfNeeded(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max * 88 / 100
	tail := max * 4 / 100
	return s[:head] +
		fmt.Sprintf("\n[...%d chars truncated by AiRouter...]\n", len(s)-head-tail) +
		s[len(s)-tail:]
}
