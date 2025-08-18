package image

// Options holds configuration for image generation flows.
// Currently it carries only the model identifier used by the backend.
// Additional fields will be added as new capabilities are introduced.
type Options struct {
	Model string
}

// NewOptions constructs an Options value using the provided model identifier.
func NewOptions(model string) Options {
	return Options{Model: model}
}
