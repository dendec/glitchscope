package config

import (
	"encoding/json"
	"testing"
)

func TestLanguageRoundTrip(t *testing.T) {
	for _, language := range AllLanguages() {
		data, err := json.Marshal(language)
		if err != nil {
			t.Fatal(err)
		}
		var got Language
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got != language {
			t.Fatalf("round trip = %q, want %q", got, language)
		}
	}
}

func TestLanguageRejectsUnknown(t *testing.T) {
	var language Language
	if err := json.Unmarshal([]byte(`"xx"`), &language); err == nil {
		t.Fatal("unknown language was accepted")
	}
}

func TestDefaultLanguage(t *testing.T) {
	if got := DefaultSettings().UI.Language; got != English {
		t.Fatalf("default language = %q, want %q", got, English)
	}
}
