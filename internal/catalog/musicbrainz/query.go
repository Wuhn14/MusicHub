package musicbrainz

import "strings"

// luceneSpecial lists the characters that have a meaning in the Lucene query
// syntax used by the MusicBrainz search. "&&" and "||" are handled by
// escaping each of their characters.
const luceneSpecial = `+-&|!(){}[]^"~*?:\/`

// escapeLucene backslash-escapes Lucene operators so that user input is
// searched literally (for example "AC/DC" or "a:b").
func escapeLucene(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if strings.ContainsRune(luceneSpecial, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapePhrase escapes a value placed between double quotes in a fielded
// Lucene query: inside a phrase only the quote and the backslash matter.
func escapePhrase(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
