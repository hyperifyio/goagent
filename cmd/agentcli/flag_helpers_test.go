package main

import "testing"

func TestFloat64FlexFlag_SetAndString(t *testing.T) {
    var value float64
    var wasSet bool
    f := &float64FlexFlag{dst: &value, set: &wasSet}
    if err := f.Set(" 1.25 "); err != nil {
        t.Fatalf("Set returned error: %v", err)
    }
    if !wasSet {
        t.Fatalf("expected wasSet=true after Set")
    }
    if value != 1.25 {
        t.Fatalf("value mismatch: got %v want %v", value, 1.25)
    }
    if s := f.String(); s != "1.25" {
        t.Fatalf("String() mismatch: got %q want %q", s, "1.25")
    }
}

func TestIntFlexFlag_SetAndString(t *testing.T) {
    var value int
    var wasSet bool
    f := &intFlexFlag{dst: &value, set: &wasSet}
    if err := f.Set(" 42 "); err != nil {
        t.Fatalf("Set returned error: %v", err)
    }
    if !wasSet {
        t.Fatalf("expected wasSet=true after Set")
    }
    if value != 42 {
        t.Fatalf("value mismatch: got %v want %v", value, 42)
    }
    if s := f.String(); s != "42" {
        t.Fatalf("String() mismatch: got %q want %q", s, "42")
    }
}

func TestStringSliceFlag_SetAndString(t *testing.T) {
    var ss stringSliceFlag
    if s := ss.String(); s != "" {
        t.Fatalf("initial String() should be empty, got %q", s)
    }
    if err := ss.Set("alpha"); err != nil {
        t.Fatalf("Set alpha: %v", err)
    }
    if err := ss.Set("beta"); err != nil {
        t.Fatalf("Set beta: %v", err)
    }
    if s := ss.String(); s != "alpha,beta" {
        t.Fatalf("String() mismatch: got %q want %q", s, "alpha,beta")
    }
}
