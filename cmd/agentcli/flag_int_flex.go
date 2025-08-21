package main

import (
	"strconv"
	"strings"
)

// intFlexFlag wires an int destination and records if it was set via flag.
type intFlexFlag struct {
	dst *int
	set *bool
}

func (f *intFlexFlag) String() string {
	if f == nil || f.dst == nil {
		return "0"
	}
	return strconv.Itoa(*f.dst)
}

func (f *intFlexFlag) Set(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if f.dst != nil {
		*f.dst = v
	}
	if f.set != nil {
		*f.set = true
	}
	return nil
}
