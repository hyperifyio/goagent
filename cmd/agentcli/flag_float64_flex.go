package main

import (
	"strconv"
	"strings"
)

// float64FlexFlag wires a float64 destination and records if it was set via flag.
type float64FlexFlag struct {
	dst *float64
	set *bool
}

func (f *float64FlexFlag) String() string {
	if f == nil || f.dst == nil {
		return ""
	}
	return strconv.FormatFloat(*f.dst, 'f', -1, 64)
}

func (f *float64FlexFlag) Set(s string) error {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
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
