package image

import "testing"

func TestNewOptions_SetsModel(t *testing.T) {
	opt := NewOptions("foo")
	if opt.Model != "foo" {
		t.Fatalf("Model=%q; want foo", opt.Model)
	}
}
