package main

import (
    "bufio"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path"
    "path/filepath"
    "regexp"
    "strings"
)

type searchInput struct {
    Query      string   `json:"query"`
    Regex      bool     `json:"regex"`
    Globs      []string `json:"globs"`
    MaxResults int      `json:"maxResults"`
}

type searchMatch struct {
    Path    string `json:"path"`
    Line    int    `json:"line"`
    Col     int    `json:"col"`
    Preview string `json:"preview"`
}

type searchOutput struct {
    Matches   []searchMatch `json:"matches"`
    Truncated bool          `json:"truncated"`
}

func main() {
    inBytes, err := io.ReadAll(os.Stdin)
    if err != nil {
        failf("read stdin: %v", err)
    }
    var in searchInput
    if err := json.Unmarshal(inBytes, &in); err != nil {
        failf("invalid json: %v", err)
    }
    if strings.TrimSpace(in.Query) == "" {
        failf("query is required")
    }

    var re *regexp.Regexp
    if in.Regex {
        var err error
        re, err = regexp.Compile(in.Query)
        if err != nil {
            failf("invalid regex: %v", err)
        }
    }

    hasGlobs := len(in.Globs) > 0

    var out searchOutput
    max := in.MaxResults
    if max <= 0 {
        max = int(^uint(0) >> 1)
    }

    walkErr := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
        if err != nil {
            return nil
        }
        if d.IsDir() {
            if d.Name() == ".git" {
                return filepath.SkipDir
            }
            return nil
        }

        if hasGlobs && !matchesAnyGlob(p, in.Globs) {
            return nil
        }

        f, err := os.Open(p)
        if err != nil {
            return nil
        }
        defer f.Close()

        rel := p
        if strings.HasPrefix(p, "./") {
            rel = p[2:]
        }
        rel = filepath.ToSlash(rel)

        scanner := bufio.NewScanner(f)
        buf := make([]byte, 0, 1024*1024)
        scanner.Buffer(buf, 10*1024*1024)

        lineNum := 0
        for scanner.Scan() {
            lineNum++
            line := scanner.Text()

            if in.Regex {
                indices := re.FindAllStringIndex(line, -1)
                for _, idx := range indices {
                    if len(out.Matches) >= max {
                        out.Truncated = true
                        return errors.New("truncated")
                    }
                    col := idx[0] + 1
                    out.Matches = append(out.Matches, searchMatch{
                        Path:    rel,
                        Line:    lineNum,
                        Col:     col,
                        Preview: line,
                    })
                }
            } else {
                from := 0
                for {
                    pos := strings.Index(line[from:], in.Query)
                    if pos == -1 {
                        break
                    }
                    if len(out.Matches) >= max {
                        out.Truncated = true
                        return errors.New("truncated")
                    }
                    col := from + pos + 1
                    out.Matches = append(out.Matches, searchMatch{
                        Path:    rel,
                        Line:    lineNum,
                        Col:     col,
                        Preview: line,
                    })
                    from = from + pos + 1
                }
            }
        }
        return nil
    })

    if walkErr != nil && walkErr.Error() != "truncated" {
        _ = walkErr
    }

    enc := json.NewEncoder(os.Stdout)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(out); err != nil {
        failf("encode output: %v", err)
    }
}

func failf(format string, a ...any) {
    _, _ = fmt.Fprintf(os.Stderr, format+"\n", a...)
    os.Exit(1)
}

func matchesAnyGlob(p string, patterns []string) bool {
    s := filepath.ToSlash(strings.TrimPrefix(p, "./"))
    for _, pat := range patterns {
        if pat == "" {
            continue
        }
        if strings.HasPrefix(pat, "**/") {
            rest := strings.TrimPrefix(pat, "**/")
            if ok, _ := path.Match(rest, path.Base(s)); ok {
                return true
            }
            continue
        }
        if ok, _ := path.Match(pat, s); ok {
            return true
        }
    }
    return false
}
