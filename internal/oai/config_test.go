package oai

import "testing"

func TestResolveImageConfig_InheritFromMainWhenUnset(t *testing.T) {
	img, baseSrc, keySrc := ResolveImageConfig("", "", "https://api.example.com/v1", "sk-main-1234")
	if img.BaseURL != "https://api.example.com/v1" || baseSrc != "inherit" {
		t.Fatalf("base inherit failed: %+v %s", img, baseSrc)
	}
	if img.APIKey != "sk-main-1234" || keySrc != "inherit" {
		t.Fatalf("key inherit failed: %+v %s", img, keySrc)
	}
}

func TestResolveImageConfig_EnvOverridesInherit(t *testing.T) {
	t.Setenv("OAI_IMAGE_BASE_URL", "https://img.example.com/v1")
	t.Setenv("OAI_IMAGE_API_KEY", "sk-img-9999")
	img, baseSrc, keySrc := ResolveImageConfig("", "", "https://api.example.com/v1", "sk-main-1234")
	if img.BaseURL != "https://img.example.com/v1" || baseSrc != "env" {
		t.Fatalf("base env failed: %+v %s", img, baseSrc)
	}
	if img.APIKey != "sk-img-9999" || keySrc != "env" {
		t.Fatalf("key env failed: %+v %s", img, keySrc)
	}
}

func TestResolveImageConfig_FlagBeatsEnv(t *testing.T) {
	t.Setenv("OAI_IMAGE_BASE_URL", "https://env-should-not-win")
	img, baseSrc, keySrc := ResolveImageConfig("https://flag-base/v1", "sk-flag-0000", "https://api.example.com/v1", "sk-main-1234")
	if img.BaseURL != "https://flag-base/v1" || baseSrc != "flag" {
		t.Fatalf("base flag failed: %+v %s", img, baseSrc)
	}
	if img.APIKey != "sk-flag-0000" || keySrc != "flag" {
		t.Fatalf("key flag failed: %+v %s", img, keySrc)
	}
}

func TestResolveImageConfig_FallbackToOpenAIKey(t *testing.T) {
	t.Setenv("OAI_IMAGE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "sk-openai-7777")
	img, _, keySrc := ResolveImageConfig("", "", "https://base", "")
	if img.APIKey != "sk-openai-7777" || keySrc != "env:OPENAI_API_KEY" {
		t.Fatalf("fallback to OPENAI_API_KEY failed: %+v %s", img, keySrc)
	}
}

func TestMaskAPIKeyLast4(t *testing.T) {
	if MaskAPIKeyLast4("") != "" {
		t.Fatalf("expected empty for empty input")
	}
	if got := MaskAPIKeyLast4("abcd"); got != "****abcd" {
		t.Fatalf("expected ****abcd, got %s", got)
	}
	if got := MaskAPIKeyLast4("sk-verylong-xyz1"); got != "****xyz1" {
		t.Fatalf("expected last4 masked, got %s", got)
	}
}
