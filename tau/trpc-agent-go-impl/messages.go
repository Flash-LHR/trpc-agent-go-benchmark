package main

import (
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func messagesFromTau(messages []tauMessage) ([]model.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("generate command requires at least one message")
	}
	out := make([]model.Message, 0, len(messages))
	for i, msg := range messages {
		converted, err := messageFromTau(msg)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", i, err)
		}
		out = append(out, converted)
	}
	return out, nil
}

func messageFromTau(msg tauMessage) (model.Message, error) {
	switch msg.Role {
	case "user":
		return model.NewUserMessage(*msg.Content), nil
	case "tool":
		id := strings.TrimSpace(msg.ID)
		return model.NewToolMessage(id, "", *msg.Content), nil
	default:
		return model.Message{}, fmt.Errorf("unsupported tau input message role %q", msg.Role)
	}
}
