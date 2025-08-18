package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type inputSpec struct {
	Prompt    string `json:"prompt"`
	N         int    `json:"n"`
	Size      string `json:"size"`
	Model     string `json:"model"`
	ReturnB64 bool   `json:"return_b64"`
	Save      *struct {
		Dir      string `json:"dir"`
		Basename string `json:"basename"`
		Ext      string `json:"ext"`
	} `json:"save"`
}

var sizeRe = regexp.MustCompile(`^\d{3,4}x\d{3,4}$`)

func main() {
	if err := run(); err != nil {
		msg := strings.TrimSpace(err.Error())
		_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": msg})
		os.Exit(1)
	}
}

func run() error {
	inBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(strings.TrimSpace(string(inBytes))) == 0 {
		return errors.New("missing json input")
	}
	var in inputSpec
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return errors.New("prompt is required")
	}
	if in.N == 0 {
		in.N = 1
	}
	if in.N < 1 || in.N > 4 {
		return errors.New("n must be between 1 and 4")
	}
	if in.Size == "" {
		in.Size = "1024x1024"
	}
	if !sizeRe.MatchString(in.Size) {
		return errors.New("size must match ^\\d{3,4}x\\d{3,4}$")
	}
	if in.Model == "" {
		in.Model = "gpt-image-1"
	}
	if !in.ReturnB64 {
		if in.Save == nil || strings.TrimSpace(in.Save.Dir) == "" {
			return errors.New("save.dir is required when return_b64=false")
		}
		if filepath.IsAbs(in.Save.Dir) {
			return errors.New("save.dir must be repo-relative")
		}
		clean := filepath.Clean(in.Save.Dir)
		if strings.HasPrefix(clean, "..") {
			return errors.New("save.dir escapes repository root")
		}
		if in.Save.Basename == "" {
			in.Save.Basename = "img"
		}
		if in.Save.Ext == "" {
			in.Save.Ext = "png"
		}
		if in.Save.Ext != "png" {
			return errors.New("ext must be 'png'")
		}
	}

	_ = http.Client{Timeout: httpTimeout()}
	// Full HTTP behavior, retries, decoding, and output will be implemented in subsequent steps per checklist.
	return errors.New("not implemented: HTTP call and output handling")
}

func httpTimeout() time.Duration {
	to := strings.TrimSpace(os.Getenv("OAI_HTTP_TIMEOUT"))
	if to == "" {
		return 120 * time.Second
	}
	if d, err := time.ParseDuration(to); err == nil {
		return d
	}
	return 120 * time.Second
}
