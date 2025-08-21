package main

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/hyperifyio/goagent/internal/oai"
)

func TestParseSavedMessages_ArrayAndWrapper(t *testing.T) {
    msgs := []oai.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}}
    // Array format
    arr, _ := json.Marshal(msgs)
    gotMsgs, img, err := parseSavedMessages(arr)
    if err != nil { t.Fatalf("array parse error: %v", err) }
    if len(gotMsgs) != 2 || img != "" {
        t.Fatalf("array parse: got %d msgs img=%q", len(gotMsgs), img)
    }
    // Wrapper format
    wrapper := map[string]any{"messages": msgs, "image_prompt": " draw this "}
    wb, _ := json.Marshal(wrapper)
    gotMsgs, img, err = parseSavedMessages(wb)
    if err != nil { t.Fatalf("wrapper parse error: %v", err) }
    if len(gotMsgs) != 2 || img != "draw this" {
        t.Fatalf("wrapper parse: got %d msgs img=%q", len(gotMsgs), img)
    }
}

func TestBuildAndWriteSavedMessages(t *testing.T) {
    msgs := []oai.Message{{Role: "system", Content: "S"}}
    w := buildMessagesWrapper(msgs, " ")
    b, err := json.Marshal(w)
    if err != nil { t.Fatalf("marshal wrapper: %v", err) }
    var m map[string]any
    if err := json.Unmarshal(b, &m); err != nil { t.Fatalf("unmarshal: %v", err) }
    if _, ok := m["messages"]; !ok { t.Fatalf("missing messages in wrapper") }
    if _, ok := m["image_prompt"]; ok { t.Fatalf("unexpected image_prompt when empty") }
    // writeSavedMessages writes pretty JSON
    d := t.TempDir()
    p := filepath.Join(d, "out.json")
    if err := writeSavedMessages(p, msgs, "a cat"); err != nil { t.Fatalf("writeSavedMessages: %v", err) }
    if _, err := os.Stat(p); err != nil { t.Fatalf("expected file written: %v", err) }
    content, _ := os.ReadFile(p)
    if !json.Valid(content) { t.Fatalf("written content not valid JSON") }
}
