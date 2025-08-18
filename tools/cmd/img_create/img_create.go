package main

import (
    "bytes"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "time"
)

type inputSpec struct {
	Prompt    string `json:"prompt"`
	N         int    `json:"n"`
	Size      string `json:"size"`
	Model     string `json:"model"`
	ReturnB64 bool   `json:"return_b64"`
    // Optional extras that are shallow-merged into the request body
    // after validation as string->primitive. Unknown or non-primitive
    // values are dropped. Core keys (model, prompt, n, size, response_format)
    // are never overridden by extras.
    Extras    map[string]any `json:"extras"`
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

    // Prepare API URL and headers
    baseURL := strings.TrimRight(firstNonEmpty(os.Getenv("OAI_IMAGE_BASE_URL"), os.Getenv("OAI_BASE_URL"), ""), "/")
    if baseURL == "" {
        return errors.New("missing OAI_IMAGE_BASE_URL or OAI_BASE_URL")
    }
    url := baseURL + "/v1/images/generations"

    reqBody := map[string]any{
        "model":           in.Model,
        "prompt":          in.Prompt,
        "n":               in.N,
        "size":            in.Size,
        "response_format": "b64_json",
    }
    // Merge sanitized extras without allowing overrides of core keys
    if len(in.Extras) > 0 {
        safe := sanitizeExtras(in.Extras)
        for k, v := range safe {
            switch k {
            case "model", "prompt", "n", "size", "response_format":
                // do not override core keys
            default:
                reqBody[k] = v
            }
        }
    }
    bodyBytes, err := json.Marshal(reqBody)
    if err != nil {
        return fmt.Errorf("marshal request: %w", err)
    }

    client := &http.Client{Timeout: httpTimeout()}
    var lastErr error
    var resp *http.Response
    for attempt := 0; attempt < 3; attempt++ {
        req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
        if err != nil {
            return fmt.Errorf("new request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        if key := strings.TrimSpace(os.Getenv("OAI_API_KEY")); key != "" {
            req.Header.Set("Authorization", "Bearer "+key)
        }
        resp, err = client.Do(req)
        if err != nil {
            lastErr = err
        } else {
            // Read all for reuse if needed
            defer resp.Body.Close()
            if shouldRetryStatus(resp.StatusCode) && attempt < 2 {
                // drain body
                _, _ = io.Copy(io.Discard, resp.Body)
                time.Sleep(backoffDelay(attempt))
                continue
            }
            break
        }
        if attempt < 2 {
            time.Sleep(backoffDelay(attempt))
        }
    }
    if resp == nil {
        return fmt.Errorf("http error: %v", lastErr)
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("read response: %w", err)
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        // Try to extract error message from JSON
        var obj map[string]any
        if json.Unmarshal(body, &obj) == nil {
            if msg, ok := obj["error"].(string); ok && msg != "" {
                return errors.New(msg)
            }
            if errobj, ok := obj["error"].(map[string]any); ok {
                if m, ok2 := errobj["message"].(string); ok2 && m != "" {
                    return errors.New(m)
                }
            }
        }
        return fmt.Errorf("api status %d", resp.StatusCode)
    }

    // Parse success body: { data: [ { b64_json: "..." }, ... ] }
    var apiResp struct {
        Data []struct {
            B64 string `json:"b64_json"`
        } `json:"data"`
        Model string `json:"model,omitempty"`
    }
    if err := json.Unmarshal(body, &apiResp); err != nil {
        return fmt.Errorf("parse response: %w", err)
    }
    if len(apiResp.Data) == 0 {
        return errors.New("no images returned")
    }

    if in.ReturnB64 {
        debug := isTruthyEnv("IMG_CREATE_DEBUG_B64") || isTruthyEnv("DEBUG_B64")
        type img struct {
            B64  string `json:"b64"`
            Hint string `json:"hint,omitempty"`
        }
        out := struct {
            Images []img `json:"images"`
        }{Images: make([]img, 0, len(apiResp.Data))}
        for _, d := range apiResp.Data {
            if debug {
                out.Images = append(out.Images, img{B64: d.B64})
            } else {
                out.Images = append(out.Images, img{B64: "", Hint: "b64 elided"})
            }
        }
        return writeJSON(out)
    }

    // Save to disk
    dir := filepath.Clean(in.Save.Dir)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return fmt.Errorf("mkdir %s: %w", dir, err)
    }
    if strings.Contains(in.Save.Basename, "/") || strings.Contains(in.Save.Basename, string(filepath.Separator)) {
        return errors.New("basename must not contain path separators")
    }
    saved := make([]struct {
        Path   string `json:"path"`
        Bytes  int    `json:"bytes"`
        Sha256 string `json:"sha256"`
    }, 0, len(apiResp.Data))
    for i, d := range apiResp.Data {
        // Decode base64 JSON field
        // Standard encoding per OpenAI: it is standard base64
        imgBytes, decErr := decodeStdB64(d.B64)
        if decErr != nil {
            return fmt.Errorf("decode b64 image %d: %w", i+1, decErr)
        }
        fname := fmt.Sprintf("%s_%03d.%s", in.Save.Basename, i+1, in.Save.Ext)
        finalPath := filepath.Join(dir, fname)
        tmpPath := filepath.Join(dir, ".tmp-"+fname+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
        if err := os.WriteFile(tmpPath, imgBytes, 0o644); err != nil {
            return fmt.Errorf("write temp file: %w", err)
        }
        if err := os.Rename(tmpPath, finalPath); err != nil {
            // best-effort cleanup
            _ = os.Remove(tmpPath)
            return fmt.Errorf("rename: %w", err)
        }
        sum := sha256.Sum256(imgBytes)
        saved = append(saved, struct {
            Path   string `json:"path"`
            Bytes  int    `json:"bytes"`
            Sha256 string `json:"sha256"`
        }{Path: finalPath, Bytes: len(imgBytes), Sha256: hex.EncodeToString(sum[:])})
    }
    out := struct {
        Saved []struct {
            Path   string `json:"path"`
            Bytes  int    `json:"bytes"`
            Sha256 string `json:"sha256"`
        } `json:"saved"`
        N     int    `json:"n"`
        Size  string `json:"size"`
        Model string `json:"model"`
    }{Saved: saved, N: len(saved), Size: in.Size, Model: in.Model}
    return writeJSON(out)
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

func firstNonEmpty(values ...string) string {
    for _, v := range values {
        if strings.TrimSpace(v) != "" {
            return v
        }
    }
    return ""
}

func shouldRetryStatus(code int) bool {
    if code == 429 {
        return true
    }
    if code >= 500 {
        return true
    }
    return false
}

func backoffDelay(attempt int) time.Duration {
    switch attempt {
    case 0:
        return 250 * time.Millisecond
    case 1:
        return 500 * time.Millisecond
    default:
        return 1 * time.Second
    }
}

func isTruthyEnv(key string) bool {
    v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
    switch v {
    case "1", "true", "yes", "y", "on":
        return true
    default:
        return false
    }
}

func decodeStdB64(s string) ([]byte, error) {
    return base64.StdEncoding.DecodeString(s)
}

func writeJSON(v any) error {
    b, err := json.Marshal(v)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    fmt.Println(string(b))
    return nil
}

// sanitizeExtras filters a map to only include string keys with primitive
// JSON types: string, float64 (numbers), bool. It also allows nulls and
// rejects nested arrays/objects to keep the request predictable.
func sanitizeExtras(in map[string]any) map[string]any {
    out := make(map[string]any, len(in))
    for k, v := range in {
        if strings.TrimSpace(k) == "" {
            continue
        }
        switch tv := v.(type) {
        case string:
            out[k] = tv
        case bool:
            out[k] = tv
        case float64:
            // json.Unmarshal decodes all numbers into float64 by default
            out[k] = tv
        case int, int32, int64, uint, uint32, uint64:
            // In practice, numbers arrive as float64, but accept ints as well
            out[k] = tv
        case nil:
            out[k] = nil
        default:
            // drop arrays, maps, and unknown types
        }
    }
    return out
}
