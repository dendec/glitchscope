package i18n

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

var englishRegionCodes = buildEnglishRegionCodes()

// DisplayRegionName returns a region's localized name for an ISO 3166 code.
// It returns an empty string when the code or localized name is unavailable.
func (c Catalog) DisplayRegionName(code string) string {
	region, err := language.ParseRegion(strings.TrimSpace(code))
	if err != nil || !region.IsCountry() {
		return ""
	}
	return displayDictionary(c.language).Regions().Name(region.Canonicalize())
}

// DisplayRegionNameFromEnglish maps a Radio Browser country name to its
// localized CLDR display name when the name has a known ISO region code.
func (c Catalog) DisplayRegionNameFromEnglish(name string) string {
	code := englishRegionCodes[normalizeDisplayName(name)]
	if code == "" {
		return ""
	}
	return c.DisplayRegionName(code)
}

func displayDictionary(language Language) *display.Dictionary {
	switch language {
	case English:
		return display.English
	case Russian:
		return display.Russian
	case SimplifiedChinese:
		return display.SimplifiedChinese
	case Japanese:
		return display.Japanese
	case Korean:
		return display.Korean
	case Spanish:
		return display.Spanish
	case German:
		return display.German
	case French:
		return display.French
	case Indonesian:
		return display.Indonesian
	case Turkish:
		return display.Turkish
	case TraditionalChinese:
		return display.TraditionalChinese
	case PortugueseBrazil:
		return display.BrazilianPortuguese
	case Vietnamese:
		return display.Vietnamese
	case Thai:
		return display.Thai
	case Malay:
		return display.Malay
	default:
		return display.English
	}
}

func buildEnglishRegionCodes() map[string]string {
	codes := make(map[string]string)
	for _, region := range language.Supported.Regions() {
		if !region.IsCountry() || len(region.String()) != 2 {
			continue
		}
		addDisplayCode(codes, display.English.Regions().Name(region), region.String())
	}
	for name, code := range map[string]string{
		"the russian federation":   "RU",
		"russian federation":       "RU",
		"united states of america": "US",
		"the united states":        "US",
		"the netherlands":          "NL",
		"the gambia":               "GM",
	} {
		addDisplayCode(codes, name, code)
	}
	return codes
}

func addDisplayCode(codes map[string]string, name, code string) {
	key := normalizeDisplayName(name)
	if key != "" && codes[key] == "" {
		codes[key] = code
	}
}

func normalizeDisplayName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
