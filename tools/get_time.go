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
	TZ string `json:"tz"`
}

type output struct {
	TZ   string `json:"tz"`
	ISO  string `json:"iso"`
	Unix int64  `json:"unix"`
}

func main() {
	inBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(1)
	}
	if len(strings.TrimSpace(string(inBytes))) == 0 {
		inBytes = []byte("{}")
	}
	var in input
	if err := json.Unmarshal(inBytes, &in); err != nil {
		fmt.Fprintf(os.Stderr, "bad json: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(in.TZ) == "" {
		in.TZ = "UTC"
	}
	loc, err := time.LoadLocation(in.TZ)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid timezone: %v\n", err)
		os.Exit(1)
	}
	now := time.Now().In(loc)
	out := output{TZ: in.TZ, ISO: now.Format(time.RFC3339), Unix: now.Unix()}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}
