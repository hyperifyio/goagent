package oai

import "testing"

func TestClampTemperature_Range(t *testing.T) {
    // Below min -> min
    if v := clampTemperature(0.01); v != minTemperature {
        t.Fatalf("below min: got %v want %v", v, minTemperature)
    }
    // Above max -> max
    if v := clampTemperature(5.0); v != maxTemperature {
        t.Fatalf("above max: got %v want %v", v, maxTemperature)
    }
    // Inside range -> unchanged
    if v := clampTemperature(0.7); v != 0.7 {
        t.Fatalf("inside range changed: got %v", v)
    }
}

func TestEffectiveTemperatureForModel_SupportedAndUnsupported(t *testing.T) {
    // Unsupported model -> false and 0 value
    if v, ok := EffectiveTemperatureForModel("o3-mini", 0.9); ok || v != 0 {
        t.Fatalf("expected unsupported with zero value, got %v %v", v, ok)
    }
    // Supported -> clamped and true
    if v, ok := EffectiveTemperatureForModel("oss-gpt-20b", 9.0); !ok || v != maxTemperature {
        t.Fatalf("expected clamped max for supported: %v %v", v, ok)
    }
}

func TestNudgedTemperature_ClampsAndRespectsSupport(t *testing.T) {
    // Unsupported -> omitted
    if v, ok := NudgedTemperature("o4-heavy", 0.5, 0.1); ok || v != 0 {
        t.Fatalf("expected omit for unsupported, got %v %v", v, ok)
    }
    // Supported -> apply and clamp
    if v, ok := NudgedTemperature("oss-gpt-20b", 0.95, 0.2); !ok || v != maxTemperature {
        t.Fatalf("expected clamped to max, got %v %v", v, ok)
    }
}
