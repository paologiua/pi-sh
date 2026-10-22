package diag

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLine bounds one diagnostic line, like OpenSSH's fixed message buffer.
const MaxLine = 1024

var (
	// SDP or ICE material: redact from the first marker to the end of the text.
	sdpPattern = regexp.MustCompile(`(?is)(\bv=0\b|\bo=-\s|\ba=(candidate|ice-ufrag|ice-pwd|fingerprint|ice-options|setup|mid|sctp-port|group)\b|\bcandidate:\s*\d).*`)
	// Any URL, including scheme-only ICE URLs; credentials or capabilities often
	// live in userinfo, paths or queries.
	urlPattern  = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]*://[^\s"'<>]*|\b(stuns?|turns?):[^\s"'<>]+`)
	pairPattern = regexp.MustCompile(`(?i)\b(password|passwd|passphrase|pwd|totp|otp|one[_-]?time[_-]?code|code|token|access[_-]?token|refresh[_-]?token|id[_-]?token|secret|client[_-]?secret|cookie|set-cookie|session[_-]?id|sessionid|session|authorization|credential|credentials|csrf[_-]?token|csrf|api[_-]?key|ufrag|ice-pwd|signature|sig|key)\s*([:=])\s*("[^"]*"|'[^']*'|[^\s,;&]+)`)
	authPattern = regexp.MustCompile(`(?i)\b(bearer|basic|digest)\s+[A-Za-z0-9._~+/=\-]+`)
	jwtPattern  = regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+(\.[A-Za-z0-9_\-]*)?`)
	mailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	opaque      = regexp.MustCompile(`[A-Za-z0-9_+=\-]{32,}`)
	tokenSyntax = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]{0,47}$`)
)

// Sanitize is the redaction backstop applied to every emitted line and to error
// text. It removes SDP/ICE material, URLs, credential-like key/value pairs,
// authorization headers, JWT and long opaque tokens and email addresses, then
// replaces control and formatting characters (escape sequences, bidi controls,
// invalid UTF-8) with '?', and bounds the result to MaxLine bytes.
func Sanitize(s string) string {
	s = sdpPattern.ReplaceAllString(s, "[redacted-sdp]")
	s = urlPattern.ReplaceAllString(s, "[redacted-url]")
	s = pairPattern.ReplaceAllString(s, "$1$2[redacted]")
	s = authPattern.ReplaceAllString(s, "$1 [redacted]")
	s = jwtPattern.ReplaceAllString(s, "[redacted-token]")
	s = mailPattern.ReplaceAllString(s, "[redacted-email]")
	s = opaque.ReplaceAllStringFunc(s, func(t string) string {
		if strings.ContainsAny(t, "0123456789") {
			return "[redacted-token]"
		}
		return t
	})
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		if r == utf8.RuneError && n <= 1 || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = '?'
		}
		if b.Len()+utf8.RuneLen(r) > MaxLine {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stateToken admits only short enumerated identifiers such as WebRTC state
// names or login stage constants; anything else is redacted.
func stateToken(s string) string {
	if tokenSyntax.MatchString(s) {
		return s
	}
	return "[redacted]"
}
