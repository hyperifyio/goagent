package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type input struct {
	Timezone string `json:"timezone"`
    // Backward-compatible alias
    TZ string `json:"tz"`
}

type output struct {
	Timezone string `json:"timezone"`
	ISO8601  string `json:"iso8601"`
}

func main() {
	inBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stdout, "{\"error\":\"read stdin: %s\"}\n", escape(err.Error()))
		return
	}
	var in input
	if len(strings.TrimSpace(string(inBytes))) == 0 {
		inBytes = []byte("{}")
	}
    if err := json.Unmarshal(inBytes, &in); err != nil {
		fmt.Fprintf(os.Stdout, "{\"error\":\"bad json: %s\"}\n", escape(err.Error()))
		return
	}
    // Prefer canonical 'timezone'; allow backward-compatible alias 'tz'
    if strings.TrimSpace(in.Timezone) == "" && strings.TrimSpace(in.TZ) != "" {
        in.Timezone = in.TZ
    }
    if strings.TrimSpace(in.Timezone) == "" {
		fmt.Printf("{\"error\":\"missing timezone\"}\n")
		return
	}
	loc, err := time.LoadLocation(in.Timezone)
	if err != nil {
		fmt.Fprintf(os.Stdout, "{\"error\":\"invalid timezone: %s\"}\n", escape(err.Error()))
		return
	}
	now := time.Now().In(loc).Format(time.RFC3339)
	out := output{Timezone: in.Timezone, ISO8601: now}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
