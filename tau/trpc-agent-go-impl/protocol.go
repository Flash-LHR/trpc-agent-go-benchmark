package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func serve(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	dec.UseNumber()
	enc := json.NewEncoder(out)
	var rt *agentRuntime
	for {
		var cmd commandRequest
		if err := dec.Decode(&cmd); err != nil {
			if errors.Is(err, io.EOF) {
				return closeRuntime(rt)
			}
			return fmt.Errorf("decode command: %w", err)
		}
		shouldStop, err := handleCommand(ctx, enc, &rt, cmd)
		if err != nil {
			return err
		}
		if shouldStop {
			return nil
		}
	}
}

func handleCommand(
	ctx context.Context,
	enc *json.Encoder,
	rt **agentRuntime,
	cmd commandRequest,
) (bool, error) {
	switch cmd.Type {
	case "start":
		return false, handleStart(enc, rt, cmd.Config)
	case "generate":
		return false, handleGenerate(ctx, enc, *rt, cmd.Messages)
	case "close":
		if err := closeRuntime(*rt); err != nil {
			return false, writeProtocolError(enc, err)
		}
		*rt = nil
		return true, enc.Encode(generateResponse{Type: "closed"})
	default:
		return false, writeProtocolError(enc, fmt.Errorf("unsupported command type %q", cmd.Type))
	}
}

func handleStart(enc *json.Encoder, rt **agentRuntime, cfg agentConfig) error {
	if err := closeRuntime(*rt); err != nil {
		return writeProtocolError(enc, err)
	}
	next, err := newAgentRuntime(cfg)
	if err != nil {
		return writeProtocolError(enc, err)
	}
	*rt = next
	return enc.Encode(generateResponse{Type: "started"})
}

func handleGenerate(
	ctx context.Context,
	enc *json.Encoder,
	rt *agentRuntime,
	messages []tauMessage,
) error {
	if rt == nil {
		return writeProtocolError(enc, errors.New("agent runtime is not started"))
	}
	resp, err := rt.generate(ctx, messages)
	if err != nil {
		return writeProtocolError(enc, err)
	}
	return enc.Encode(resp)
}

func closeRuntime(rt *agentRuntime) error {
	if rt == nil {
		return nil
	}
	return rt.close()
}

func writeProtocolError(enc *json.Encoder, err error) error {
	return enc.Encode(generateResponse{
		Type:  "error",
		Error: err.Error(),
	})
}
