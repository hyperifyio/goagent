package main

import (
	"strconv"
	"strings"
	"time"
)

// float64FlexFlag wires a float64 destination and records if it was set via flag.
type float64FlexFlag struct {
	dst *float64
	set *bool
}

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

// durationFlexFlag wires a duration destination and records if it was set via flag.
type durationFlexFlag struct {
	dst *time.Duration
	set *bool
}

func (f durationFlexFlag) String() string {
	if f.dst == nil {
		return ""
	}
	return f.dst.String()
}

func (f durationFlexFlag) Set(s string) error {
	d, err := parseDurationFlexible(s)
	if err != nil {
		return err
	}
	*f.dst = d
	if f.set != nil {
		*f.set = true
	}
	return nil
}

// stringSliceFlag implements flag.Value to collect repeatable string flags into a slice.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}
