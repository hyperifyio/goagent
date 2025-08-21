package main

import (
    "encoding/json"
    "os"
    "strings"
    "testing"
)

// TestPrintConfig_IncludesImageModel verifies that -print-config output contains
// image.model and that precedence is flag > env > default.
func TestPrintConfig_IncludesImageModel(t *testing.T) {
    type imageBlock struct {
        Model string `json:"model"`
    }
    type cfgOut struct {
        Image imageBlock `json:"image"`
    }

    run := func(t *testing.T, env map[string]string, args ...string) string {
        t.Helper()
        // Save and restore env and args
        oldArgs := os.Args
        defer func() { os.Args = oldArgs }()
        var unsetKeys []string
        oldEnv := make(map[string]string)
        for k, v := range env {
            if ov, ok := os.LookupEnv(k); ok {
                oldEnv[k] = ov
            } else {
                unsetKeys = append(unsetKeys, k)
            }
            if err := os.Setenv(k, v); err != nil {
                t.Fatalf("setenv %s: %v", k, err)
            }
        }
        defer func() {
            for k, v := range oldEnv {
                _ = os.Setenv(k, v)
            }
            for _, k := range unsetKeys {
                _ = os.Unsetenv(k)
            }
        }()

        os.Args = append([]string{"agentcli"}, args...)
        cfg, exitOn := parseFlags()
        if exitOn != 0 {
            t.Fatalf("parseFlags exitOn=%d parseError=%q", exitOn, cfg.parseError)
        }
        var b strings.Builder
        code := printResolvedConfig(cfg, &b)
        if code != 0 {
            t.Fatalf("printResolvedConfig exit code=%d", code)
        }
        var out cfgOut
        if err := json.Unmarshal([]byte(b.String()), &out); err != nil {
            t.Fatalf("unmarshal output: %v; raw=%s", err, b.String())
        }
        return out.Image.Model
    }

    t.Run("default", func(t *testing.T) {
        got := run(t, map[string]string{"OAI_IMAGE_MODEL": ""}, "-print-config")
        if got != "gpt-image-1" {
            t.Fatalf("image.model default: got %q want %q", got, "gpt-image-1")
        }
    })

    t.Run("env_only", func(t *testing.T) {
        got := run(t, map[string]string{"OAI_IMAGE_MODEL": "img-env"}, "-print-config")
        if got != "img-env" {
            t.Fatalf("image.model env: got %q want %q", got, "img-env")
        }
    })

    t.Run("flag_overrides_env", func(t *testing.T) {
        got := run(t, map[string]string{"OAI_IMAGE_MODEL": "img-env"}, "-print-config", "-image-model", "img-flag")
        if got != "img-flag" {
            t.Fatalf("image.model flag overrides env: got %q want %q", got, "img-flag")
        }
    })
}
