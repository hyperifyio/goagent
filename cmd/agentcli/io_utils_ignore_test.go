package main

import (
    "errors"
    "testing"
)

func TestIgnoreError_NoPanic(t *testing.T) {
    ignoreError(nil)
    ignoreError(errors.New("some error"))
}
