package oai

import "testing"

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
