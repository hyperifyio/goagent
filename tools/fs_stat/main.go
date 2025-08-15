package main

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "time"
)

type fsStatInput struct {
    Path            string `json:"path"`
    FollowSymlinks  bool   `json:"followSymlinks"`
    Hash            string `json:"hash"`
}

type fsStatOutput struct {
    Exists    bool   `json:"exists"`
    Type      string `json:"type"`
    SizeBytes int64  `json:"sizeBytes"`
    ModeOctal string `json:"modeOctal"`
    ModTime   string `json:"modTime"`
    SHA256    string `json:"sha256,omitempty"`
}

func main() {
    var in fsStatInput
    dec := json.NewDecoder(os.Stdin)
    if err := dec.Decode(&in); err != nil {
        exitError(fmt.Errorf("invalid JSON input: %w", err))
        return
    }

    if err := validateRepoRelativePath(in.Path); err != nil {
        exitError(err)
        return
    }

    // Choose stat variant based on followSymlinks.
    var info os.FileInfo
    var err error
    if in.FollowSymlinks {
        info, err = os.Stat(in.Path)
    } else {
        info, err = os.Lstat(in.Path)
    }
    if err != nil {
        if os.IsNotExist(err) {
            // Report non-existent cleanly with exit code 0.
            emitJSON(fsStatOutput{Exists: false})
            return
        }
        exitError(fmt.Errorf("stat failed: %w", err))
        return
    }

    fileType := detectType(info)

    out := fsStatOutput{
        Exists:    true,
        Type:      fileType,
        SizeBytes: info.Size(),
        ModeOctal: fmt.Sprintf("%04o", info.Mode().Perm()),
        ModTime:   info.ModTime().UTC().Format(time.RFC3339),
    }

    if strings.EqualFold(in.Hash, "sha256") && fileType == "file" {
        if sum, err := sha256OfFile(in.Path); err == nil {
            out.SHA256 = sum
        }
    }

    emitJSON(out)
}

func validateRepoRelativePath(p string) error {
    if p == "" {
        return errors.New(`{"error":"path is required"}`)
    }
    if filepath.IsAbs(p) {
        return errors.New(`{"error":"absolute paths are not allowed"}`)
    }
    clean := filepath.Clean(p)
    if strings.HasPrefix(clean, "..") || strings.Contains(clean, string(filepath.Separator)+".."+string(filepath.Separator)) {
        return errors.New(`{"error":"path must be repo-relative without parent traversal"}`)
    }
    return nil
}

func detectType(info os.FileInfo) string {
    mode := info.Mode()
    switch {
    case mode.IsRegular():
        return "file"
    case mode.IsDir():
        return "dir"
    case mode&os.ModeSymlink != 0:
        return "symlink"
    default:
        return "other"
    }
}

func sha256OfFile(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil {
        return "", err
    }
    defer func() { _ = f.Close() }()
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil {
        return "", err
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}

func emitJSON(v any) {
    enc := json.NewEncoder(os.Stdout)
    enc.SetEscapeHTML(false)
    _ = enc.Encode(v)
}

func exitError(err error) {
    // Standardize as single-line JSON error on stderr.
    msg := err.Error()
    // If the message already looks like a JSON object, write as-is; otherwise wrap.
    if strings.HasPrefix(msg, "{") && strings.HasSuffix(strings.TrimSpace(msg), "}") {
        _, _ = fmt.Fprintln(os.Stderr, msg)
    } else {
        b, _ := json.Marshal(map[string]string{"error": msg})
        _, _ = os.Stderr.Write(append(b, '\n'))
    }
    os.Exit(1)
}
