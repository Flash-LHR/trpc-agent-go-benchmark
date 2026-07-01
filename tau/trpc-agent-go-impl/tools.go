package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type dynamicTool struct {
	decl *tool.Declaration
}

func buildTools(specs []openAIToolSchema) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(specs))
	for _, spec := range specs {
		decl, err := declarationFromOpenAITool(spec)
		if err != nil {
			return nil, err
		}
		tools = append(tools, &dynamicTool{decl: decl})
	}
	return tools, nil
}

func declarationFromOpenAITool(spec openAIToolSchema) (*tool.Declaration, error) {
	fn := spec.Function
	name := strings.TrimSpace(fn.Name)
	inputSchema, err := rawSchemaToToolSchema(fn.Parameters)
	if err != nil {
		return nil, fmt.Errorf("tool %s input schema: %w", name, err)
	}
	return &tool.Declaration{
		Name:        name,
		Description: strings.TrimSpace(fn.Description),
		InputSchema: inputSchema,
	}, nil
}

func rawSchemaToToolSchema(raw json.RawMessage) (*tool.Schema, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	normalized := normalizeSchema(value)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	var schema tool.Schema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, err
	}
	return &schema, nil
}

func normalizeSchema(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return normalizeSchemaObject(v)
	case []any:
		for i := range v {
			v[i] = normalizeSchema(v[i])
		}
		return v
	default:
		return v
	}
}

func normalizeSchemaObject(obj map[string]any) map[string]any {
	mergeFirstNonNullAnyOf(obj)
	if typeList, ok := obj["type"].([]any); ok {
		obj["type"] = firstNonNullType(typeList)
	}
	for key, value := range obj {
		obj[key] = normalizeSchema(value)
	}
	return obj
}

func mergeFirstNonNullAnyOf(obj map[string]any) {
	options, ok := obj["anyOf"].([]any)
	if !ok {
		return
	}
	for _, option := range options {
		candidate, ok := option.(map[string]any)
		if !ok || candidate["type"] == "null" {
			continue
		}
		for key, value := range candidate {
			if _, exists := obj[key]; !exists {
				obj[key] = value
			}
		}
		break
	}
	delete(obj, "anyOf")
}

func firstNonNullType(values []any) string {
	for _, value := range values {
		s, ok := value.(string)
		if ok && s != "null" {
			return s
		}
	}
	return ""
}

func (t *dynamicTool) Declaration() *tool.Declaration {
	return t.decl
}
