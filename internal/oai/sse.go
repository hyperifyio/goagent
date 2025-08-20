package oai

import (
    "bufio"
    "io"
)

// newLineReader returns a closure that reads one line (terminated by \n) from r each call.
func newLineReader(r io.Reader) func() (string, error) {
    br := bufio.NewReader(r)
    return func() (string, error) {
        b, err := br.ReadBytes('\n')
        if err != nil {
            return "", err
        }
        return string(b), nil
    }
}
