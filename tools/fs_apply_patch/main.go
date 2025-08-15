package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type applyPatchInput struct {
	UnifiedDiff string `json:"unifiedDiff"`
}

func main() {
	data, _ := io.ReadAll(os.Stdin)
	_ = json.Unmarshal(data, &applyPatchInput{})
	_, _ = fmt.Fprintln(os.Stderr, `{"error":"NOT_IMPLEMENTED"}`)
	os.Exit(2)
}
