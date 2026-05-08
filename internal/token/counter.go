package token

import (
	"encoding/json"
	"fmt"
)

// CountRequest is the unified token counting request
type CountRequest struct {
	Model    string   `json:"model"`
	Messages []Message `json:"messages"`
	System   string   `json:"system,omitempty"`
}

// Message represents a message for token counting
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CountResponse is the token counting response
type CountResponse struct {
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

// CountTokens estimates tokens for the given request based on model type
func CountTokens(req CountRequest) (CountResponse, error) {
	promptTokens := estimateTokens(req.System, req.Messages)

	return CountResponse{
		Model:            req.Model,
		PromptTokens:     promptTokens,
		CompletionTokens: 0,
		TotalTokens:      promptTokens,
	}, nil
}

// estimateTokens estimates token count using a simple heuristic
// Different models have different tokenization, this provides a rough estimate
func estimateTokens(system string, messages []Message) int {
	total := 0

	// System prompt
	if system != "" {
		total += countTokens(system)
	}

	// Each message has overhead
	for _, msg := range messages {
		total += 4 // message overhead
		total += countTokens(msg.Content)
		if msg.Role == "user" {
			total += 1
		} else {
			total += 1
		}
	}

	total += 2 // final assistant overhead
	return total
}

// countTokens provides a rough token estimation
// Based on the approximation that 1 token ≈ 4 characters for English text
// and 1 token ≈ 1.5 characters for Chinese text
func countTokens(text string) int {
	if text == "" {
		return 0
	}

	// Count Chinese characters
	chineseCount := 0
	englishChars := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
		} else {
			englishChars++
		}
	}

	// Chinese: ~1.5 chars per token, English: ~4 chars per token
	return chineseCount/2 + englishChars/4
}

// CountTokensForClaude estimates tokens for Claude API format
func CountTokensForClaude(body []byte) (CountResponse, error) {
	var req struct {
		Model     string `json:"model"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		System string `json:"system,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return CountResponse{}, err
	}

	messages := make([]Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = Message{Role: msg.Role, Content: msg.Content}
	}

	promptTokens := estimateTokens(req.System, messages)

	return CountResponse{
		Model:            req.Model,
		PromptTokens:     promptTokens,
		CompletionTokens: 0,
		TotalTokens:      promptTokens,
	}, nil
}

// CountTokensForOpenAI estimates tokens for OpenAI chat completions format
func CountTokensForOpenAI(body []byte) (CountResponse, error) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return CountResponse{}, err
	}

	messages := make([]Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = Message{Role: msg.Role, Content: msg.Content}
	}

	promptTokens := estimateTokens("", messages)

	return CountResponse{
		Model:            req.Model,
		PromptTokens:     promptTokens,
		CompletionTokens: 0,
		TotalTokens:      promptTokens,
	}, nil
}

// ModelInfo returns token counting info for a model
func ModelInfo(model string) map[string]interface{} {
	return map[string]interface{}{
		"model":           model,
		"tokenizer":       "estimated",
		"tokens_per_char": 0.25,
		"note":            "Token counts are estimates based on character heuristics",
	}
}

// FormatEstimation returns a human-readable estimation string
func FormatEstimation(tokens int) string {
	return fmt.Sprintf("~%d tokens (estimated)", tokens)
}

// TokenResult is an alias of CountResponse for use in proxy handler
type TokenResult = CountResponse

// NewResult creates a TokenResult with estimated tokens based on response body length
func NewResult(model string, bodyLen int) *TokenResult {
	estimatedTokens := bodyLen / 4
	return &TokenResult{
		Model:            model,
		PromptTokens:     0,
		CompletionTokens: estimatedTokens,
		TotalTokens:      estimatedTokens,
	}
}
