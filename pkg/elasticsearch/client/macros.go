package client

import (
	"strconv"
	"strings"
	"time"
)

// interpolateSearchBody expands interval macros in an encoded search request
// body, covering both backend-built aggregations and user-authored raw DSL
// queries.
//
// interval_msms is not user-facing: addDateHistogramAgg emits the
// $__interval_msms placeholder for "auto" date histogram intervals because
// fixed_interval needs a single-unit value, and the always-milliseconds form
// (e.g. "500ms") is valid for any interval, unlike $__interval whose
// time.Duration formatting can produce multi-unit values such as "1m30s".
//
// macropro is deliberately not used here: its SQL-flavoured parser treats a
// '(' immediately after a macro name as an argument list and consumes text up
// to the matching ')', corrupting parenthesised JSON or painless text after a
// macro token. None of these macros take arguments, so a plain boundary-aware
// replacement is both simpler and correct. Tokens are replaced
// longest-name-first so $__interval never matches inside the _ms forms.
func interpolateSearchBody(body string, interval time.Duration) (string, error) {
	intervalMS := interval.Milliseconds()
	if intervalMS <= 0 {
		intervalMS = 1000
	}
	ms := strconv.FormatInt(intervalMS, 10)

	replacements := []struct {
		token string
		value string
	}{
		{"$__interval_msms", ms + "ms"},
		{"$__interval_ms", ms},
		{"$__interval", interval.String()},
	}
	for _, r := range replacements {
		body = replaceMacroToken(body, r.token, r.value)
	}
	return body, nil
}

// replaceMacroToken replaces every occurrence of token in body with value,
// except where the token is immediately followed by an identifier character,
// which makes it part of a longer name such as $__intervalfoo.
func replaceMacroToken(body, token, value string) string {
	var sb strings.Builder
	for {
		i := strings.Index(body, token)
		if i < 0 {
			break
		}
		end := i + len(token)
		if end < len(body) && isIdentifierChar(body[end]) {
			sb.WriteString(body[:end])
			body = body[end:]
			continue
		}
		sb.WriteString(body[:i])
		sb.WriteString(value)
		body = body[end:]
	}
	if sb.Len() == 0 {
		return body
	}
	sb.WriteString(body)
	return sb.String()
}

func isIdentifierChar(c byte) bool {
	return c == '_' ||
		('0' <= c && c <= '9') ||
		('a' <= c && c <= 'z') ||
		('A' <= c && c <= 'Z')
}
