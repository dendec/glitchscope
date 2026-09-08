package config

import (
	"encoding/json"
	"fmt"
)

// Language selects the language used for user-facing text.
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

func (l Language) String() string {
	switch l {
	case English:
		return "English"
	case Russian:
		return "Русский"
	case SimplifiedChinese:
		return "简体中文"
	case Japanese:
		return "日本語"
	case Korean:
		return "한국어"
	case Spanish:
		return "Español"
	case German:
		return "Deutsch"
	case French:
		return "Français"
	case Indonesian:
		return "Bahasa Indonesia"
	case Turkish:
		return "Türkçe"
	case TraditionalChinese:
		return "繁體中文"
	case PortugueseBrazil:
		return "Português (Brasil)"
	case Vietnamese:
		return "Tiếng Việt"
	case Thai:
		return "ไทย"
	case Malay:
		return "Bahasa Melayu"
	default:
		return "Unknown"
	}
}

func (l Language) MarshalJSON() ([]byte, error) { return json.Marshal(string(l)) }

func (l *Language) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	candidate := Language(value)
	if err := candidate.Validate(); err != nil {
		return err
	}
	*l = candidate
	return nil
}

func (l Language) Validate() error {
	switch l {
	case English, Russian, SimplifiedChinese, Japanese, Korean, Spanish, German, French, Indonesian, Turkish,
		TraditionalChinese, PortugueseBrazil, Vietnamese, Thai, Malay:
		return nil
	default:
		return fmt.Errorf("unknown language: %s", l)
	}
}

// AllLanguages returns supported languages in Settings display order.
func AllLanguages() []Language {
	return []Language{
		English, Russian, SimplifiedChinese, TraditionalChinese, Japanese, Korean,
		Vietnamese, Thai, Indonesian, Malay, PortugueseBrazil, Spanish, German,
		French, Turkish,
	}
}
