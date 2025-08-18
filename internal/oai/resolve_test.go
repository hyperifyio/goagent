package oai

import (
	"os"
	"strconv"
	"strings"
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

func TestParseDurationFlexible_ErrorCases(t *testing.T) {
	// Empty string should return an error
	if _, err := parseDurationFlexible(""); err == nil {
		t.Fatalf("expected error for empty input")
	}
	// Negative seconds should return range error
	if _, err := parseDurationFlexible("-5"); err == nil {
		t.Fatalf("expected error for negative seconds")
	}
	// Non-parsable should return syntax error
	if _, err := parseDurationFlexible("nonsense"); err == nil {
		t.Fatalf("expected error for nonsense input")
	}
}

func TestResolveInt_EnvWhitespaceAndDefaultFallback(t *testing.T) {
	// Env value with whitespace parses after trimming
	if v, src := ResolveInt(false, 0, " 7 ", nil, 1); v != 7 || src != "env" {
		t.Fatalf("env whitespace trim failed: v=%d src=%s", v, src)
	}
	// Env parse error with nil inherit falls back to default
	if v, src := ResolveInt(false, 0, "bogus", nil, 3); v != 3 || src != "default" {
		t.Fatalf("default fallback failed: v=%d src=%s", v, src)
	}
}

func TestResolveBool_EnvParseErrorDefaultFallback(t *testing.T) {
	// Env parse error with nil inherit falls back to default
	if v, src := ResolveBool(false, false, "notbool", nil, true); v != true || src != "default" {
		t.Fatalf("default fallback for bool failed: v=%v src=%s", v, src)
	}
}

func TestResolveString_TrimEnvAndInherit(t *testing.T) {
	inh := "  inherited  "
	// Env wins and is trimmed
	if v, src := ResolveString("", "  env  ", &inh, "def"); v != "env" || src != "env" {
		t.Fatalf("env trim failed: v=%q src=%s", v, src)
	}
	// When env empty, inherit applies and is trimmed
	if v, src := ResolveString("", "", &inh, "def"); v != strings.TrimSpace(inh) || src != "inherit" {
		t.Fatalf("inherit trim failed: v=%q src=%s", v, src)
	}
}

func TestResolveDuration_EnvWhitespace(t *testing.T) {
	inh := time.Second
	if v, src := ResolveDuration(false, 0, " 1s ", &inh, time.Second); v != time.Second || src != "env" {
		t.Fatalf("env whitespace duration failed: v=%s src=%s", v, src)
	}
}

func TestResolveInt_TrimsDoesNotAcceptNonDigits(t *testing.T) {
	// Guard: numeric string with suffix should not parse; falls back to inherit
	inh := 42
	if v, src := ResolveInt(false, 0, "5s", &inh, 0); v != inh || src != "inherit" {
		t.Fatalf("inherit after bad int parse failed: v=%d src=%s", v, src)
	}
}

func TestParseDurationFlexible_EnvEquivalence_WhitespaceAndDigits(t *testing.T) {
	t.Setenv("X", " 10 ")
	d, err := parseDurationFlexible(strings.TrimSpace(os.Getenv("X")))
	if err != nil || d != 10*time.Second {
		t.Fatalf("parse integer seconds with whitespace failed: %v %s", err, d)
	}
	// Also ensure large but valid seconds parse
	s := strconv.Itoa(15)
	if d2, err := parseDurationFlexible(s); err != nil || d2 != 15*time.Second {
		t.Fatalf("parse integer seconds failed: %v %s", err, d2)
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
