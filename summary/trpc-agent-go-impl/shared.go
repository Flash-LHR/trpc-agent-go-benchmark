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
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TokenUsage stores detailed token usage for a single LLM call.
type TokenUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// ToolCallStats stores how many times each tool was invoked during one run.
type ToolCallStats struct {
	Counts map[string]int `json:"counts,omitempty"`
}

func (s *ToolCallStats) increment(toolName string) {
	if s == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return
	}
	if s.Counts == nil {
		s.Counts = make(map[string]int)
	}
	s.Counts[toolName]++
}

func (s *ToolCallStats) Count(toolName string) int {
	if s == nil || s.Counts == nil {
		return 0
	}
	return s.Counts[toolName]
}

func consumeEvents(evtCh <-chan *event.Event) (string, *TokenUsage) {
	response, usage, _ := consumeEventsWithToolStats(evtCh)
	return response, usage
}

func consumeEventsWithToolStats(evtCh <-chan *event.Event) (string, *TokenUsage, *ToolCallStats) {
	var response strings.Builder
	usage := &TokenUsage{}
	toolStats := &ToolCallStats{}
	seenToolCalls := make(map[string]struct{})

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
		recordToolCalls(evt.Response, toolStats, seenToolCalls)
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
	return response.String(), usage, toolStats
}

func recordToolCalls(
	resp *model.Response,
	toolStats *ToolCallStats,
	seenToolCalls map[string]struct{},
) {
	if resp == nil {
		return
	}
	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			recordToolCall(tc, toolStats, seenToolCalls)
		}
		for _, tc := range choice.Delta.ToolCalls {
			recordToolCall(tc, toolStats, seenToolCalls)
		}
	}
}

func recordToolCall(
	tc model.ToolCall,
	toolStats *ToolCallStats,
	seenToolCalls map[string]struct{},
) {
	name := strings.TrimSpace(tc.Function.Name)
	if name == "" {
		return
	}
	if tc.ID != "" {
		if _, ok := seenToolCalls[tc.ID]; ok {
			return
		}
		seenToolCalls[tc.ID] = struct{}{}
	}
	toolStats.increment(name)
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
