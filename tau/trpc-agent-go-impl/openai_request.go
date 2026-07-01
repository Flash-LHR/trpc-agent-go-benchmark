package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

const llmLogDirEnv = "TAU_TRPC_AGENT_GO_LLM_LOG_DIR"

var requestLogCounter uint64

func buildModel(req agentConfig) (model.Model, error) {
	opts := []openai.Option{}
	if apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); apiKey != "" {
		opts = append(opts, openai.WithAPIKey(apiKey))
	}
	if baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	if logDir := strings.TrimSpace(os.Getenv(llmLogDirEnv)); logDir != "" {
		opts = append(opts, openai.WithChatRequestJSONCallback(chatRequestLogCallback(logDir)))
	}
	return openai.New(req.Model, opts...), nil
}

func chatRequestLogCallback(logDir string) openai.ChatRequestJSONCallbackFunc {
	return func(_ context.Context, chatRequestJSON []byte, marshalErr error) {
		payload := map[string]any{"timestamp": time.Now().Format(time.RFC3339Nano)}
		if marshalErr != nil {
			payload["marshal_error"] = marshalErr.Error()
		}
		if len(bytes.TrimSpace(chatRequestJSON)) > 0 {
			var request any
			dec := json.NewDecoder(bytes.NewReader(chatRequestJSON))
			dec.UseNumber()
			if err := dec.Decode(&request); err != nil {
				payload["decode_error"] = err.Error()
				payload["request_json"] = string(chatRequestJSON)
			} else {
				payload["request"] = request
			}
		}
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return
		}
		seq := atomic.AddUint64(&requestLogCounter, 1)
		name := fmt.Sprintf("%s_%d_%06d_trpc_agent_go_request.json", time.Now().Format("20060102_150405_000000000"), os.Getpid(), seq)
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return
		}
		_ = os.WriteFile(filepath.Join(logDir, name), append(encoded, '\n'), 0o644)
	}
}
