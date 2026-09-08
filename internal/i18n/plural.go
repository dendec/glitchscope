package i18n

// Plural selects a locale-specific suffix (.one/.few/.many/.other).
func (c Catalog) Plural(key Key, n int, args ...any) string {
	form := pluralForm(c.language, n)
	return c.Format(Key(string(key)+"."+form), append([]any{n}, args...)...)
}

func pluralForm(language Language, n int) string {
	form := "other"
	switch language {
	case Russian:
		mod10, mod100 := n%10, n%100
		switch {
		case mod10 == 1 && mod100 != 11:
			form = "one"
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			form = "few"
		case mod10 == 0 || mod10 >= 5 || (mod100 >= 11 && mod100 <= 14):
			form = "many"
		}
	case English, German, Spanish:
		if n == 1 {
			form = "one"
		}
	case French:
		if n == 0 || n == 1 {
			form = "one"
		}
	}
	return form
}
