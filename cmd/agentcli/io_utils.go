package main

import (
	"fmt"
	"io"
)

// ignoreError is used to explicitly acknowledge and ignore expected errors
// in places where failure is handled via alternative control flow.
func ignoreError(_ error) {}

// safeFprintln writes a line to w and intentionally ignores write errors.
func safeFprintln(w io.Writer, a ...any) {
	if _, err := fmt.Fprintln(w, a...); err != nil {
		return
	}
}

// safeFprintf writes formatted text to w and intentionally ignores write errors.
func safeFprintf(w io.Writer, format string, a ...any) {
	if _, err := fmt.Fprintf(w, format, a...); err != nil {
		return
	}
}
