package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultAPIURL = "https://api.openai.com/v1/chat/completions"
	defaultModel  = "gpt-4o-mini"
)

// Client is an OpenAI API client
type Client struct {
	apiKey     string
	apiURL     string
	model      string
	httpClient *http.Client
}

// NewClient creates a new OpenAI client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		apiURL: defaultAPIURL,
		model:  defaultModel,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithModel sets a custom model
func (c *Client) WithModel(model string) *Client {
	c.model = model
	return c
}

// ChatMessage represents a message in the chat
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents the request body for chat completions
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// ChatResponse represents the response from chat completions
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// RewriteMessage rewrites a message using AI based on the given prompt
func (c *Client) RewriteMessage(ctx context.Context, originalMessage, prompt string) (string, error) {
	systemPrompt := fmt.Sprintf(`You are a message rewriting assistant. Your task is to rewrite the following message according to these instructions:

%s

Important rules:
1. Return ONLY the rewritten message, nothing else - no explanations, no quotes around it
2. Maintain the general meaning and purpose of the original message
3. Keep any names or personal details that appear in the message
4. The message is for personal communication via Telegram`, prompt)

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Rewrite this message:\n\n%s", originalMessage)},
	}

	reqBody := ChatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   1000,
		Temperature: 0.7,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("OpenAI API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return chatResp.Choices[0].Message.Content, nil
}
