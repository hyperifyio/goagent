package oai

import (
	"os"
	"testing"
	"time"
)

func TestResolveString(t *testing.T) {
	inherit := "inh"
	v, src := ResolveString("flag", "", &inherit, "def")
	if v != "flag" || src != "flag" {
		t.Fatalf("flag precedence failed: %s %s", v, src)
	}
	v, src = ResolveString("", "env", &inherit, "def")
	if v != "env" || src != "env" {
		t.Fatalf("env precedence failed: %s %s", v, src)
	}
	v, src = ResolveString("", "", &inherit, "def")
	if v != "inh" || src != "inherit" {
		t.Fatalf("inherit precedence failed: %s %s", v, src)
	}
	v, src = ResolveString("", "", nil, "def")
	if v != "def" || src != "default" {
		t.Fatalf("default precedence failed: %s %s", v, src)
	}
}

func TestResolveInt(t *testing.T) {
	inherit := 9
	if v, src := ResolveInt(true, 7, "", &inherit, 1); v != 7 || src != "flag" {
		t.Fatalf("flag precedence failed")
	}
	if v, src := ResolveInt(false, 0, "5", &inherit, 1); v != 5 || src != "env" {
		t.Fatalf("env precedence failed")
	}
	if v, src := ResolveInt(false, 0, "bad", &inherit, 1); v != 9 || src != "inherit" {
		t.Fatalf("inherit fallback failed")
	}
	if v, src := ResolveInt(false, 0, "", nil, 1); v != 1 || src != "default" {
		t.Fatalf("default fallback failed")
	}
}

func TestResolveBool(t *testing.T) {
	inh := false
	if v, src := ResolveBool(true, true, "", &inh, false); !v || src != "flag" {
		t.Fatalf("flag precedence failed")
	}
	if v, src := ResolveBool(false, false, "true", &inh, false); !v || src != "env" {
		t.Fatalf("env precedence failed")
	}
	if v, src := ResolveBool(false, false, "bad", &inh, false); v != false || src != "inherit" {
		t.Fatalf("inherit fallback failed")
	}
	if v, src := ResolveBool(false, false, "", nil, true); v != true || src != "default" {
		t.Fatalf("default fallback failed")
	}
}

func TestResolveDuration(t *testing.T) {
	inh := 2 * time.Second
	if v, src := ResolveDuration(true, 3*time.Second, "", &inh, time.Second); v != 3*time.Second || src != "flag" {
		t.Fatalf("flag precedence failed")
	}
	if v, src := ResolveDuration(false, 0, "750ms", &inh, time.Second); v != 750*time.Millisecond || src != "env" {
		t.Fatalf("env duration parse failed: %s %s", v, src)
	}
	if v, src := ResolveDuration(false, 0, "5", &inh, time.Second); v != 5*time.Second || src != "env" {
		t.Fatalf("env integer seconds failed: %s %s", v, src)
	}
	if v, src := ResolveDuration(false, 0, "bad", &inh, time.Second); v != 2*time.Second || src != "inherit" {
		t.Fatalf("inherit fallback failed: %s %s", v, src)
	}
	if v, src := ResolveDuration(false, 0, "", nil, time.Second); v != time.Second || src != "default" {
		t.Fatalf("default fallback failed: %s %s", v, src)
	}
}

func TestResolveImageConfig_MaskAPIKeyLast4_Compatibility(t *testing.T) {
	// Sanity: ensure MaskAPIKeyLast4 still behaves as expected in config printer
	if got := MaskAPIKeyLast4("abcd"); got != "****abcd" {
		t.Fatalf("mask last4 failed: %s", got)
	}
}

func TestParseDurationFlexible_EnvEquivalence(t *testing.T) {
    // Ensure our internal parser treats env duration strings and integers equally
    t.Setenv("X", "750ms")
    if d, err := parseDurationFlexible(os.Getenv("X")); err != nil || d != 750*time.Millisecond {
        t.Fatalf("parse 750ms failed: %v %s", err, d)
    }
    t.Setenv("X", "2")
    if d, err := parseDurationFlexible(os.Getenv("X")); err != nil || d != 2*time.Second {
        t.Fatalf("parse 2 failed: %v %s", err, d)
    }
}
