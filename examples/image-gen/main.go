package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

type SaveSpec struct {
	Dir      string `json:"dir"`
	Basename string `json:"basename"`
	Ext      string `json:"ext"`
}

type Request struct {
	Prompt    string   `json:"prompt"`
	N         int      `json:"n,omitempty"`
	Size      string   `json:"size,omitempty"`
	Model     string   `json:"model,omitempty"`
	ReturnB64 bool     `json:"return_b64,omitempty"`
	Save      *SaveSpec `json:"save,omitempty"`
}

func main() {
	prompt := getenvDefault("PROMPT", "tiny illustrative banner")
	size := getenvDefault("SIZE", "1024x1024")
	n := getenvIntDefault("N", 1)
	basename := getenvDefault("BASENAME", "img")
	saveDir := getenvDefault("SAVE_DIR", "assets")
	returnB64 := os.Getenv("RETURN_B64") == "1"

	req := Request{Prompt: prompt, N: n, Size: size}
	if returnB64 {
		req.ReturnB64 = true
	} else {
		req.Save = &SaveSpec{Dir: saveDir, Basename: basename, Ext: "png"}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal request: %v\n", err)
		os.Exit(2)
	}

	bin := "./tools/bin/img_create"
	if runtime.GOOS == "windows" {
		bin = "./tools/bin/img_create.exe"
	}

	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// img_create prints a single-line JSON error to stderr; exit non-zero
		os.Exit(1)
	}
}

func getenvDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getenvIntDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var out int
	if _, err := fmt.Sscanf(v, "%d", &out); err != nil {
		return def
	}
	return out
}
