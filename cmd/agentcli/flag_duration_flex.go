package main

import "time"

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
