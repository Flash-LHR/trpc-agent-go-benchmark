package main

import "encoding/json"

type commandRequest struct {
	Type     string       `json:"type"`
	Config   agentConfig  `json:"config"`
	Messages []tauMessage `json:"messages"`
}

type agentConfig struct {
	Model          string             `json:"model"`
	Planner        string             `json:"planner"`
	SystemPrompt   string             `json:"system_prompt"`
	Tools          []openAIToolSchema `json:"tools"`
	MaxTokens      int                `json:"max_tokens"`
	Temperature    *float64           `json:"temperature"`
	TimeoutSeconds int                `json:"timeout_seconds"`
}

type tauMessage struct {
	Role      string        `json:"role"`
	Content   *string       `json:"content"`
	ID        string        `json:"id"`
	ToolCalls []tauToolCall `json:"tool_calls"`
}

type tauToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type openAIToolSchema struct {
	Type     string               `json:"type"`
	Function openAIFunctionSchema `json:"function"`
}

type openAIFunctionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type generateResponse struct {
	Type      string             `json:"type"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []responseToolCall `json:"tool_calls,omitempty"`
	Usage     *usagePayload      `json:"usage,omitempty"`
	Error     string             `json:"error,omitempty"`
}

type responseToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
