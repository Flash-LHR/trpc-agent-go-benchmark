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
	"context"
	"encoding/json"
	"os"
)

func main() {
	if err := serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		writeErrorAndExit(err)
	}
}

func writeErrorAndExit(err error) {
	_ = json.NewEncoder(os.Stdout).Encode(generateResponse{
		Type:  "error",
		Error: err.Error(),
	})
	os.Exit(1)
}
