package i18n

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCatalogsHaveIdenticalKeys(t *testing.T) {
	english, err := loadAsset(English)
	if err != nil {
		t.Fatal(err)
	}
	for language := range supportedLanguages {
		catalog, loadErr := loadAsset(language)
		if loadErr != nil {
			t.Errorf("%s: %v", language, loadErr)
			continue
		}
		if len(english) != len(catalog) {
			t.Errorf("key count: en=%d %s=%d", len(english), language, len(catalog))
		}
		for key := range english {
			if _, ok := catalog[key]; !ok {
				t.Errorf("locale %s missing %q", language, key)
			}
		}
	}
}

func TestTranslatedHelpIsEmbeddedJSON(t *testing.T) {
	for language := range supportedLanguages {
		if language == English {
			continue
		}
		data, err := HelpData(language)
		if err != nil {
			t.Errorf("%s: %v", language, err)
			continue
		}
		if !json.Valid(data) {
			t.Errorf("%s Help asset is not valid JSON", language)
		}
	}
}

func TestPluralRules(t *testing.T) {
	tests := []struct {
		language Language
		n        int
		want     string
	}{
		{English, 1, "one"},
		{English, 2, "other"},
		{Russian, 0, "many"},
		{Russian, 1, "one"},
		{Russian, 2, "few"},
		{Russian, 4, "few"},
		{Russian, 5, "many"},
		{Russian, 11, "many"},
		{Russian, 21, "one"},
		{Russian, 22, "few"},
		{Russian, 25, "many"},
		{Russian, 101, "one"},
	}
	for _, test := range tests {
		if got := pluralForm(test.language, test.n); got != test.want {
			t.Errorf("%s %d = %s, want %s", test.language, test.n, got, test.want)
		}
	}
}

// Format strings are part of the caller contract, even when their wording changes.
func TestTranslationsPreserveFormatArguments(t *testing.T) {
	pattern := regexp.MustCompile(`%(?:[0-9]+)?(?:\.[0-9]+)?[dsf]`)
	english, err := loadAsset(English)
	if err != nil {
		t.Fatal(err)
	}
	for language := range supportedLanguages {
		catalog, loadErr := loadAsset(language)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		for key, source := range english {
			want := pattern.FindAllString(source, -1)
			got := pattern.FindAllString(catalog[key], -1)
			if !slices.Equal(got, want) {
				t.Errorf("%s %s: format arguments %v, want %v", language, key, got, want)
			}
		}
	}
}

func TestInfoSizeIsTranslatedForEveryLanguage(t *testing.T) {
	const value = "1.6 MB"
	for language := range supportedLanguages {
		got := MustLoad(language).Format(InfoSize, value)
		if !strings.Contains(got, value) {
			t.Errorf("%s info.size lost its value: %q", language, got)
		}
		if strings.TrimSpace(strings.TrimSuffix(got, value)) == "" {
			t.Errorf("%s info.size has no translated label: %q", language, got)
		}
	}
}
