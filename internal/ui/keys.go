package ui

import "unicode/utf8"

// Hotkeys must work whatever keyboard layout is active. The terminal sends the
// typed character, so on a Russian layout "q" arrives as "й". We map the
// character back to the Latin key on the same physical position.
var layoutToLatin = map[rune]rune{
	// Russian ЙЦУКЕН
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y', 'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p', 'х': '[', 'ъ': ']',
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g', 'р': 'h', 'о': 'j', 'л': 'k', 'д': 'l', 'ж': ';', 'э': '\'',
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b', 'т': 'n', 'ь': 'm', 'ё': '`',
	'Й': 'Q', 'Ц': 'W', 'У': 'E', 'К': 'R', 'Е': 'T', 'Н': 'Y', 'Г': 'U', 'Ш': 'I', 'Щ': 'O', 'З': 'P', 'Х': '{', 'Ъ': '}',
	'Ф': 'A', 'Ы': 'S', 'В': 'D', 'А': 'F', 'П': 'G', 'Р': 'H', 'О': 'J', 'Л': 'K', 'Д': 'L', 'Ж': ':', 'Э': '"',
	'Я': 'Z', 'Ч': 'X', 'С': 'C', 'М': 'V', 'И': 'B', 'Т': 'N', 'Ь': 'M', 'Ё': '~',
	// Ukrainian / Belarusian letters that differ from Russian
	'і': 's', 'І': 'S', 'ї': ']', 'Ї': '}', 'є': '\'', 'Є': '"', 'ґ': '`', 'Ґ': '~', 'ў': 'o', 'Ў': 'O',
}

// latinKey converts a single typed character to its Latin-layout equivalent.
// Multi-character key names ("enter", "ctrl+c", "up") pass through unchanged.
func latinKey(k string) string {
	r, size := utf8.DecodeRuneInString(k)
	if size == 0 || size != len(k) {
		return k
	}
	if l, ok := layoutToLatin[r]; ok {
		return string(l)
	}
	return k
}
