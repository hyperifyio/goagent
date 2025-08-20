package main

import (
	"strconv"
	"strings"
)

// boolFlexFlag wires a bool destination and records if it was set via flag.
type boolFlexFlag struct {
	dst *bool
	set *bool
}

func (b *boolFlexFlag) String() string {
	if b == nil || b.dst == nil {
		return "false"
	}
	if *b.dst {
		return "true"
	}
	return "false"
}

func (b *boolFlexFlag) Set(s string) error {
	v, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if b.dst != nil {
		*b.dst = v
	}
	if b.set != nil {
		*b.set = true
	}
	return nil
}
