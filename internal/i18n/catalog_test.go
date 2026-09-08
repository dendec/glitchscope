package i18n

import (
	"encoding/json"
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
