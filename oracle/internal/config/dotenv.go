package config

import (
	"regexp"
	"strings"
)

// jsSpace is what JavaScript's \s and String.prototype.trim match: its
// WhiteSpace and LineTerminator characters, the byte order mark included.
const jsSpace = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// dotenvLine is dotenv 16's LINE regular expression (lib/main.js), with \s
// and . spelled out as JavaScript defines them. (In JavaScript, ^ and $ also
// match at U+2028 and U+2029, here only at newlines.)
var dotenvLine = func() *regexp.Regexp {
	s := `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
	dot := `[^\n\r\x{2028}\x{2029}]`
	return regexp.MustCompile(`(?m)^` + s + `*(?:export` + s + `+)?([\w.-]+)(?:` + s + `*=` + s + `*?|:` + s + `+?)` +
		`(` + s + `*'(?:\\'|[^'])*'|` + s + `*"(?:\\"|[^"])*"|` + s + "*`(?:\\\\`|[^`])*`" + `|[^#\r\n]+)?` +
		s + `*(?:#` + dot + `*)?$`)
}()

// parseDotenv parses an env file as dotenv 16, the TS oracle's loader, does:
// KEY=VALUE or KEY: VALUE lines, optionally after "export"; a value unquoted
// (up to a #, trimmed) or in single, double or backtick quotes, which may span
// lines; \n and \r expanded when the value starts with a double quote. Lines
// that don't match are skipped, a later key wins, and nothing else is
// interpreted: no $VAR expansion, no other escapes, never an error (so the
// file's content never ends up in a message).
func parseDotenv(src string) map[string]string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	out := map[string]string{}
	for _, m := range dotenvLine.FindAllStringSubmatch(src, -1) {
		value := strings.Trim(m[2], jsSpace)
		doubleQuoted := strings.HasPrefix(value, `"`)
		if n := len(value); n >= 2 && strings.ContainsRune("'\"`", rune(value[0])) && value[n-1] == value[0] {
			value = value[1 : n-1]
		}
		if doubleQuoted {
			value = strings.ReplaceAll(value, `\n`, "\n")
			value = strings.ReplaceAll(value, `\r`, "\r")
		}
		out[m[1]] = value
	}
	return out
}
