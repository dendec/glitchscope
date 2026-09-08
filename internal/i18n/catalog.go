package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
)

// Language identifies an embedded locale.
type Language string

const (
	English            Language = "en"
	Russian            Language = "ru"
	SimplifiedChinese  Language = "zh-Hans"
	Japanese           Language = "ja"
	Korean             Language = "ko"
	Spanish            Language = "es"
	German             Language = "de"
	French             Language = "fr"
	Indonesian         Language = "id"
	Turkish            Language = "tr"
	TraditionalChinese Language = "zh-Hant"
	PortugueseBrazil   Language = "pt-BR"
	Vietnamese         Language = "vi"
	Thai               Language = "th"
	Malay              Language = "ms"
)

var supportedLanguages = map[Language]bool{
	English: true, Russian: true, SimplifiedChinese: true, Japanese: true,
	Korean: true, Spanish: true, German: true, French: true,
	Indonesian: true, Turkish: true, TraditionalChinese: true,
	PortugueseBrazil: true, Vietnamese: true, Thai: true, Malay: true,
}

//go:embed assets/*.json
var assets embed.FS

// Catalog is immutable after construction.
type Catalog struct {
	language Language
	text     map[Key]string
}

func Load(language Language) (Catalog, error) {
	if !supportedLanguages[language] {
		return Catalog{}, fmt.Errorf("unsupported language %q", language)
	}
	english, err := loadAsset(English)
	if err != nil {
		return Catalog{}, err
	}
	selected := english
	if language != English {
		selected, err = loadAsset(language)
		if err != nil {
			return Catalog{}, err
		}
		for key := range english {
			if _, ok := selected[key]; !ok {
				return Catalog{}, fmt.Errorf("locale %s missing key %q", language, key)
			}
		}
	}
	return Catalog{language: language, text: selected}, nil
}

func MustLoad(language Language) Catalog {
	catalog, err := Load(language)
	if err != nil {
		panic(err)
	}
	return catalog
}

func loadAsset(language Language) (map[Key]string, error) {
	data, err := assets.ReadFile("assets/" + string(language) + ".json")
	if err != nil {
		return nil, fmt.Errorf("read locale %s: %w", language, err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode locale %s: %w", language, err)
	}
	result := make(map[Key]string, len(raw))
	for key, value := range raw {
		if value == "" {
			return nil, fmt.Errorf("locale %s has empty key %q", language, key)
		}
		result[Key(key)] = value
	}
	return result, nil
}

// HelpData returns the embedded Help document for a translated locale.
func HelpData(language Language) ([]byte, error) {
	if language == English || !supportedLanguages[language] {
		return nil, fmt.Errorf("unsupported language %q", language)
	}
	data, err := assets.ReadFile("assets/" + string(language) + "-help.json")
	if err != nil {
		return nil, fmt.Errorf("read Help locale %s: %w", language, err)
	}
	return data, nil
}

func (c Catalog) Language() Language { return c.language }

func (c Catalog) Text(key Key) string {
	if value, ok := c.text[key]; ok {
		return value
	}
	english, err := loadAsset(English)
	if err == nil {
		return english[key]
	}
	return ""
}

func (c Catalog) Format(key Key, args ...any) string { return fmt.Sprintf(c.Text(key), args...) }
