package codex

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ── Anthropic request types ───────────────────────────────────────────────────

type AnthropicRequest struct {
	Model         string          `json:"model"`
	Messages      []AMessage      `json:"messages"`
	System        json.RawMessage `json:"system,omitempty"`
	MaxTokens     int             `json:"max_tokens,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Temp          *float64        `json:"temperature,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
}

type AMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// extractText extracts plain text from a content field that may be a string
// or an array of content blocks (Anthropic Messages API format).
func extractText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

// extractSystem extracts plain text from a system field that may be a string
// or an array of system content blocks (Anthropic Messages API format).
func extractSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// BuildCodexRequest converts an Anthropic request to a Codex request.
// model is the resolved default model from Config.
func BuildCodexRequest(req *AnthropicRequest, model string) *CodexRequest {
	instructions := extractSystem(req.System)
	if instructions == "" {
		instructions = "You are a helpful assistant."
	}

	var input []InputItem
	for _, msg := range req.Messages {
		role := "user"
		contentType := "input_text"
		if msg.Role == "assistant" {
			role = "assistant"
			contentType = "output_text"
		}
		input = append(input, InputItem{
			Type: "message",
			Role: role,
			Content: []ContentPart{
				{Type: contentType, Text: extractText(msg.Content)},
			},
		})
	}

	return &CodexRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
		Stream:       req.Stream,
		Store:        false,
		Temperature:  req.Temp,
	}
}

// ── Anthropic response types ──────────────────────────────────────────────────

type AnthropicResponse struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Content      []AContentBlock `json:"content"`
	Model        string          `json:"model"`
	StopReason   string          `json:"stop_reason"`
	StopSequence *string         `json:"stop_sequence"`
	Usage        AUsage          `json:"usage"`
}

type AContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type codexResponseBody struct {
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// TranslateResponse converts a Codex non-streaming response to Anthropic format.
func TranslateResponse(body []byte, model string) (*AnthropicResponse, error) {
	var cr codexResponseBody
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("decode codex response: %w", err)
	}

	var text string
	for _, item := range cr.Output {
		if item.Type == "message" {
			for _, block := range item.Content {
				if block.Type == "output_text" || block.Type == "text" {
					text += block.Text
				}
			}
		}
	}

	return &AnthropicResponse{
		ID:   "msg_" + newID(),
		Type: "message",
		Role: "assistant",
		Content: []AContentBlock{
			{Type: "text", Text: text},
		},
		Model:      model,
		StopReason: "end_turn",
		Usage: AUsage{
			InputTokens:  cr.Usage.InputTokens,
			OutputTokens: cr.Usage.OutputTokens,
		},
	}, nil
}

// ── SSE streaming translation ─────────────────────────────────────────────────

type sseChunk struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

// AnthropicSSEWriter translates Codex SSE events to Anthropic SSE format.
type AnthropicSSEWriter struct {
	write        func([]byte) (int, error)
	flush        func()
	msgID        string
	model        string
	headerSent   bool
	blockStarted bool
	outputTokens int
	inputTokens  int
}

func NewAnthropicSSEWriter(write func([]byte) (int, error), flush func(), model string) *AnthropicSSEWriter {
	return &AnthropicSSEWriter{
		write: write,
		flush: flush,
		msgID: "msg_" + newID(),
		model: model,
	}
}

func (w *AnthropicSSEWriter) send(event string, data any) {
	b, _ := json.Marshal(data)
	w.write([]byte("event: " + event + "\ndata: " + string(b) + "\n\n"))
	w.flush()
}

func (w *AnthropicSSEWriter) ensureHeader() {
	if w.headerSent {
		return
	}
	w.headerSent = true
	w.send("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": w.msgID, "type": "message", "role": "assistant",
			"content": []any{}, "model": w.model,
			"stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	})
	w.send("content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]string{"type": "text", "text": ""},
	})
	w.send("ping", map[string]string{"type": "ping"})
	w.blockStarted = true
}

func (w *AnthropicSSEWriter) HandleChunk(raw string) {
	var chunk sseChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		return
	}
	w.ensureHeader()
	switch chunk.Type {
	case "response.output_text.delta":
		if chunk.Delta != "" {
			w.outputTokens += len([]rune(chunk.Delta))/4 + 1
			w.send("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": 0,
				"delta": map[string]string{"type": "text_delta", "text": chunk.Delta},
			})
		}
	case "response.completed", "response.done":
		if chunk.Usage != nil {
			w.inputTokens = chunk.Usage.InputTokens
			w.outputTokens = chunk.Usage.OutputTokens
		}
	}
}

func (w *AnthropicSSEWriter) Finish() {
	if !w.headerSent {
		w.ensureHeader()
	}
	if w.blockStarted {
		w.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	}
	w.send("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": w.outputTokens},
	})
	w.send("message_stop", map[string]string{"type": "message_stop"})
}

// ── CollectSSEToAnthropic: non-streaming Anthropic response from SSE ──────────

func CollectSSEToAnthropic(r io.Reader, model string) (*AnthropicResponse, error) {
	ch := make(chan SSEEvent, 32)
	go ReadSSE(r, ch)

	var text string
	var inputTokens, outputTokens int
	for ev := range ch {
		if ev.Data == "" || ev.Data == "[DONE]" {
			continue
		}
		var chunk sseChunk
		if json.Unmarshal([]byte(ev.Data), &chunk) != nil {
			continue
		}
		switch chunk.Type {
		case "response.output_text.delta":
			text += chunk.Delta
		case "response.completed", "response.done":
			if chunk.Usage != nil {
				inputTokens = chunk.Usage.InputTokens
				outputTokens = chunk.Usage.OutputTokens
			}
		}
	}
	return &AnthropicResponse{
		ID:   "msg_" + newID(),
		Type: "message",
		Role: "assistant",
		Content: []AContentBlock{
			{Type: "text", Text: text},
		},
		Model:      model,
		StopReason: "end_turn",
		Usage:      AUsage{InputTokens: inputTokens, OutputTokens: outputTokens},
	}, nil
}

// ── OpenAI request/response types ────────────────────────────────────────────

type OpenAIRequest struct {
	Model     string          `json:"model"`
	Messages  []OpenAIMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
	Temp      *float64        `json:"temperature,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type OpenAIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// extractOpenAIText extracts plain text from an OpenAI content field that may
// be a string or an array of content parts (text/image_url).
func extractOpenAIText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == "text" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, "")
	}
	return ""
}

// BuildCodexRequestFromOpenAI converts an OpenAI chat request to a Codex request.
func BuildCodexRequestFromOpenAI(req *OpenAIRequest, model string) *CodexRequest {
	var system string
	var input []InputItem
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = extractOpenAIText(m.Content)
			continue
		}
		role := "user"
		contentType := "input_text"
		if m.Role == "assistant" {
			role = "assistant"
			contentType = "output_text"
		}
		input = append(input, InputItem{
			Type: "message",
			Role: role,
			Content: []ContentPart{
				{Type: contentType, Text: extractOpenAIText(m.Content)},
			},
		})
	}
	if system == "" {
		system = "You are a helpful assistant."
	}
	return &CodexRequest{
		Model:        model,
		Instructions: system,
		Input:        input,
		Stream:       req.Stream,
		Store:        false,
		Temperature:  req.Temp,
	}
}

// openAIResponseMessage is used in non-streaming responses (has "message" not "delta").
type openAIResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	SystemFingerprint *string        `json:"system_fingerprint"`
	Choices           []openAIChoice `json:"choices"`
	Usage             openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int                   `json:"index"`
	Message      openAIResponseMessage `json:"message"`
	Logprobs     *json.RawMessage      `json:"logprobs"`
	FinishReason string                `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// CollectSSEToOpenAI collects Codex SSE and returns an OpenAI chat completion response.
func CollectSSEToOpenAI(r io.Reader, model string) (*openAIResponse, error) {
	ch := make(chan SSEEvent, 32)
	go ReadSSE(r, ch)

	var text string
	var inputTokens, outputTokens int
	for ev := range ch {
		if ev.Data == "" || ev.Data == "[DONE]" {
			continue
		}
		var chunk sseChunk
		if json.Unmarshal([]byte(ev.Data), &chunk) != nil {
			continue
		}
		switch chunk.Type {
		case "response.output_text.delta":
			text += chunk.Delta
		case "response.completed", "response.done":
			if chunk.Usage != nil {
				inputTokens = chunk.Usage.InputTokens
				outputTokens = chunk.Usage.OutputTokens
			}
		}
	}
	return &openAIResponse{
		ID:      "chatcmpl-" + newID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIChoice{
			{
				Index:        0,
				Message:      openAIResponseMessage{Role: "assistant", Content: text},
				FinishReason: "stop",
			},
		},
		Usage: openAIUsage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
	}, nil
}

// ── OpenAI SSE streaming writer ───────────────────────────────────────────────

type OpenAISSEWriter struct {
	write      func([]byte) (int, error)
	flush      func()
	msgID      string
	model      string
	done       bool
	firstChunk bool
}

func NewOpenAISSEWriter(write func([]byte) (int, error), flush func(), model string) *OpenAISSEWriter {
	return &OpenAISSEWriter{
		write:      write,
		flush:      flush,
		msgID:      "chatcmpl-" + newID(),
		model:      model,
		firstChunk: true,
	}
}

func (w *OpenAISSEWriter) send(data any) {
	b, _ := json.Marshal(data)
	w.write([]byte("data: " + string(b) + "\n\n"))
	w.flush()
}

func (w *OpenAISSEWriter) HandleChunk(raw string) {
	var chunk sseChunk
	if json.Unmarshal([]byte(raw), &chunk) != nil {
		return
	}
	if chunk.Type == "response.output_text.delta" && chunk.Delta != "" {
		var delta map[string]string
		if w.firstChunk {
			delta = map[string]string{"role": "assistant", "content": chunk.Delta}
			w.firstChunk = false
		} else {
			delta = map[string]string{"content": chunk.Delta}
		}
		w.send(map[string]any{
			"id":      w.msgID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   w.model,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         delta,
					"finish_reason": nil,
				},
			},
		})
	}
}

func (w *OpenAISSEWriter) Finish() {
	if !w.done {
		w.done = true
		w.send(map[string]any{
			"id":      w.msgID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   w.model,
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"},
			},
		})
		w.write([]byte("data: [DONE]\n\n"))
		w.flush()
	}
}
