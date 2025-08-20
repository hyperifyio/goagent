package oai

// Temperature clamping and nudge helpers.

const (
    // minTemperature is the lowest allowed sampling temperature.
    minTemperature = 0.1
    // maxTemperature is the highest allowed sampling temperature.
    maxTemperature = 1.0
)

// clampTemperature returns value clamped to the inclusive range [0.1, 1.0].
func clampTemperature(value float64) float64 {
    if value < minTemperature {
        return minTemperature
    }
    if value > maxTemperature {
        return maxTemperature
    }
    return value
}

// EffectiveTemperatureForModel returns the temperature to use for the given
// model, applying the supported-model guard and clamping to the allowed range.
// The second return value is false when the model does not support temperature
// and the caller should omit the field entirely.
func EffectiveTemperatureForModel(model string, temperature float64) (float64, bool) {
    if !SupportsTemperature(model) {
        return 0, false
    }
    return clampTemperature(temperature), true
}

// NudgedTemperature applies a delta to the current temperature for supported
// models and returns the clamped result. When the target model does not support
// temperature, it returns (0, false) to indicate the field must be omitted.
func NudgedTemperature(model string, current float64, nudgeDelta float64) (float64, bool) {
    if !SupportsTemperature(model) {
        return 0, false
    }
    return clampTemperature(current + nudgeDelta), true
}
