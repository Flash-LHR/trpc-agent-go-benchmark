package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func collectResponse(events <-chan *event.Event) (*generateResponse, error) {
	var content strings.Builder
	var firstToolCalls []responseToolCall
	var usage *usagePayload
	for evt := range events {
		if evt.Error != nil {
			return nil, fmt.Errorf("agent event error: %s", evt.Error.Message)
		}
		usage = updateUsage(usage, evt.Response.Usage)
		if len(firstToolCalls) == 0 {
			calls, err := toolCallsFromResponse(evt.Response)
			if err != nil {
				return nil, err
			}
			firstToolCalls = calls
		}
		if len(firstToolCalls) == 0 {
			appendContent(&content, evt.Response)
		}
	}
	if len(firstToolCalls) > 0 {
		return &generateResponse{Type: "tool_calls", ToolCalls: firstToolCalls, Usage: usage}, nil
	}
	return &generateResponse{Type: "text", Content: strings.TrimSpace(content.String()), Usage: usage}, nil
}

func updateUsage(current *usagePayload, usage *model.Usage) *usagePayload {
	if usage == nil {
		return current
	}
	return &usagePayload{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

func toolCallsFromResponse(resp *model.Response) ([]responseToolCall, error) {
	for _, choice := range resp.Choices {
		calls, err := responseToolCalls(choice.Message.ToolCalls)
		if err != nil {
			return nil, err
		}
		if len(calls) > 0 {
			return calls, nil
		}
		calls, err = responseToolCalls(choice.Delta.ToolCalls)
		if err != nil {
			return nil, err
		}
		if len(calls) > 0 {
			return calls, nil
		}
	}
	return nil, nil
}

func responseToolCalls(calls []model.ToolCall) ([]responseToolCall, error) {
	out := make([]responseToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		id := strings.TrimSpace(call.ID)
		args, err := parseArguments(call.Function.Arguments)
		if err != nil {
			return nil, fmt.Errorf("tool call %s arguments: %w", name, err)
		}
		out = append(out, responseToolCall{
			ID:        id,
			Name:      name,
			Arguments: args,
		})
	}
	return out, nil
}

func parseArguments(raw []byte) (map[string]any, error) {
	var args map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&args); err != nil {
		return nil, err
	}
	return args, nil
}

func appendContent(b *strings.Builder, resp *model.Response) {
	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			b.WriteString(choice.Message.Content)
		}
		if choice.Delta.Content != "" {
			b.WriteString(choice.Delta.Content)
		}
	}
}
