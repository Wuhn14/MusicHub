// Package textfold normalises text for case- and accent-insensitive
// comparisons.
package textfold

import "strings"

// table maps accented Latin letters to their base letters (lower case).
var table = func() map[rune]rune {
	m := map[rune]rune{}
	for base, accented := range map[rune]string{
		'a': "àáâãäåāăą", 'c': "çćĉċč", 'd': "ďđ", 'e': "èéêëēĕėęě", 'g': "ĝğġģ", 'h': "ĥħ",
		'i': "ìíîïĩīĭįı", 'j': "ĵ", 'k': "ķ", 'l': "ĺļľŀł", 'n': "ñńņňŉ", 'o': "òóôõöøōŏő",
		'r': "ŕŗř", 's': "śŝşšß", 't': "ţťŧ", 'u': "ùúûüũūŭůűų", 'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	} {
		for _, r := range accented {
			m[r] = base
		}
	}
	return m
}()

// Fold lower-cases s, trims it and strips accents, so comparisons ignore
// all three.
func Fold(s string) string {
	return strings.Map(func(r rune) rune {
		if b, ok := table[r]; ok {
			return b
		}
		return r
	}, strings.ToLower(strings.TrimSpace(s)))
}
