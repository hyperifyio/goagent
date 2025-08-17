package oai

import "testing"

func TestClampTemperature(t *testing.T) {
    cases := []struct{
        in float64
        want float64
    }{
        {0.05, 0.1},
        {0.1, 0.1},
        {0.2, 0.2},
        {0.99, 0.99},
        {1.0, 1.0},
        {1.5, 1.0},
    }
    for _, c := range cases {
        if got := clampTemperature(c.in); got != c.want {
            t.Fatalf("clamp(%v)=%v want %v", c.in, got, c.want)
        }
    }
}

func TestEffectiveTemperatureForModel(t *testing.T) {
    // Unsupported model: false
    if _, ok := EffectiveTemperatureForModel("o3-mini", 0.7); ok {
        t.Fatalf("expected unsupported model to return ok=false")
    }
    // Supported model: clamped and true
    if got, ok := EffectiveTemperatureForModel("oss-gpt-20b", 1.5); !ok || got != 1.0 {
        t.Fatalf("expected clamped=1.0 ok=true; got %v ok=%v", got, ok)
    }
    if got, ok := EffectiveTemperatureForModel("oss-gpt-20b", 0.05); !ok || got != 0.1 {
        t.Fatalf("expected clamped=0.1 ok=true; got %v ok=%v", got, ok)
    }
}

func TestNudgedTemperature(t *testing.T) {
    // Unsupported model: no-op (omit)
    if _, ok := NudgedTemperature("o4-preview", 0.5, -0.1); ok {
        t.Fatalf("expected unsupported model to return ok=false")
    }
    // Clamp lower bound
    if got, ok := NudgedTemperature("oss-gpt-20b", 0.12, -0.1); !ok || got != 0.1 {
        t.Fatalf("nudge lower clamp got %v ok=%v", got, ok)
    }
    // Clamp upper bound
    if got, ok := NudgedTemperature("oss-gpt-20b", 0.95, 0.2); !ok || got != 1.0 {
        t.Fatalf("nudge upper clamp got %v ok=%v", got, ok)
    }
    // In-range nudge
    if got, ok := NudgedTemperature("oss-gpt-20b", 0.6, -0.1); !ok || got != 0.5 {
        t.Fatalf("nudge in-range got %v ok=%v", got, ok)
    }
}
