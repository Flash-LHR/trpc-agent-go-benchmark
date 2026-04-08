//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/event"
)

// TokenUsage stores detailed token usage for a single LLM call.
type TokenUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

func consumeEvents(evtCh <-chan *event.Event) (string, *TokenUsage) {
	var response strings.Builder
	usage := &TokenUsage{}

	for evt := range evtCh {
		if evt.Error != nil {
			continue
		}
		if evt.Response == nil {
			continue
		}
		if evt.Response.Usage != nil {
			usage.PromptTokens = evt.Response.Usage.PromptTokens
			usage.CompletionTokens = evt.Response.Usage.CompletionTokens
			usage.TotalTokens = evt.Response.Usage.TotalTokens
		}
		if len(evt.Response.Choices) == 0 {
			continue
		}
		choice := evt.Response.Choices[0]
		if choice.Message.Content != "" {
			response.WriteString(choice.Message.Content)
		}
		if choice.Delta.Content != "" {
			response.WriteString(choice.Delta.Content)
		}
	}
	return response.String(), usage
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return 0
		}
		i, err := strconv.Atoi(x)
		if err != nil {
			return 0
		}
		return i
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	default:
		return 0
	}
}

func asFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func asFloat64Slice(v any) []float64 {
	switch x := v.(type) {
	case []float64:
		return x
	case []any:
		out := make([]float64, 0, len(x))
		for _, item := range x {
			out = append(out, asFloat64(item))
		}
		return out
	default:
		return nil
	}
}

func intPtr(i int) *int { return &i }

func float64Ptr(v float64) *float64 { return &v }

// truncateStr truncates a string to maxLen characters, replacing newlines.
func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
