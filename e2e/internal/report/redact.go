package report

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"text/scanner"

	"github.com/google/shlex"
)

// Sensitive flag values and key=value pairs are scrubbed from commands, stderr
// and manifests before any report is written or uploaded.
var redactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(--password[ =])\S+`),
	regexp.MustCompile(`(?i)(--user-data[ =])\S+`),
	regexp.MustCompile(`(?i)(--key-pair-name[ =])\S+`),
	regexp.MustCompile(`(?i)((?:access[_-]?key(?:id|secret)?|secret|security[_-]?token|password|token|client-key-data|client-certificate-data|certificate-authority-data)\s*[:=]\s*)\S+`),
}

var privateConfigFlag = regexp.MustCompile(`--(?:runtime|build)-config['"]?(?:=|[ \t\r\n]+)`)

func scrub(s string) string {
	if structured, ok := scrubStructuredJSON(s); ok {
		return structured
	}
	return scrubText(s)
}

// Scrub removes sensitive values from text before it is emitted outside the
// report rendering pipeline, for example in cleanup diagnostics and logs.
func Scrub(s string) string {
	return scrub(s)
}

// RenderCommand masks private authored arguments before substitutions can
// change their shell boundaries. render must be side-effect free.
func RenderCommand(source string, render func(string) (string, error)) (string, error) {
	rendered, err := render(source)
	if err != nil {
		return "", err
	}
	masked := scrubPrivateConfigArguments(source, render)
	if masked == source {
		return rendered, nil
	}
	display, err := render(masked)
	if err != nil {
		// The original already rendered successfully. Masking can remove an
		// adjacent control action; never turn a display limitation into a
		// command failure or fall back to the private original output.
		return "***", nil
	}
	return display, nil
}

func scrubText(s string) string {
	s = scrubPrivateConfigArguments(s, nil)
	for _, re := range redactors {
		s = re.ReplaceAllString(s, "${1}***")
	}
	return s
}

// Native inline configurations are private as a whole. Adjacent shell quote
// segments belong to the same argument, including after an existing mask.
// Never execute shell text or load @file input.
func scrubPrivateConfigArguments(s string, render func(string) (string, error)) string {
	scan := s
	if render != nil {
		scan = privateConfigTemplateSource(s)
	}
	_, inputErr := shlex.Split(scan)
	for offset := 0; offset < len(s); {
		start := offset
		for start < len(scan) && strings.ContainsRune(" \t\r\n", rune(scan[start])) {
			start++
		}
		if start == len(s) {
			break
		}
		end, complete := privateConfigArgumentEnd(scan, start, 0)
		comment := scan[start] == '#'
		if comment {
			// An action can emit a newline that ends this source comment.
			// Keep the entire comment in the authored provenance interval.
			end, complete = len(scan), true
			if newline := strings.IndexByte(scan[start:], '\n'); newline >= 0 {
				end = start + newline
			}
		}
		argumentRender := render
		var renderedWord string
		resolved := render != nil && strings.Contains(s[start:end], "{{")
		if resolved {
			var certain bool
			renderedWord, certain = privateConfigRenderedArgument(s, start, end, render)
			if !certain {
				return s[:start] + "***"
			}
			argumentRender = func(string) (string, error) { return renderedWord, nil }
		}
		words, err := shlex.Split(scan[start:end])
		if !complete || err != nil {
			// Interpolation may break quoting only after a valid flag prefix.
			// Recognize that key without requiring its private value to parse.
			key := privateConfigInlineKey(scan[start:end])
			if resolved {
				key, _ = privateConfigRenderedKey(renderedWord)
			}
			if key == "--runtime-config" || key == "--build-config" {
				return s[:start] + key + "=***"
			}
			// Diagnostics need not be complete shell commands. Preserve the
			// conservative literal-flag fallback for malformed surrounding text.
			if privateConfigFlag.MatchString(scan[start:]) {
				return s[:start] + "***"
			}
			break
		}
		key, value, inline := "", "", false
		if len(words) == 1 {
			key, value, inline = strings.Cut(words[0], "=")
		} else if !comment || !resolved {
			offset = end
			continue
		}
		if resolved {
			key, inline = privateConfigRenderedKey(renderedWord)
		}
		if key != "--runtime-config" && key != "--build-config" {
			offset = end
			continue
		}
		if inputErr != nil {
			// Interpolation can close a quote before private whitespace while
			// leaving the full input malformed. A locally complete fragment
			// cannot establish where this private value actually ends.
			return s[:start] + key + "=***"
		}
		maskStart, valueStart, mask := start, start, key+"=***"
		if inline {
			if privateConfigFileReference(s[start:end], value, key+"=", argumentRender) {
				offset = end
				continue
			}
			valueStart = len(s)
			if equals := strings.IndexByte(scan[start:end], '='); equals >= 0 {
				valueStart = start + equals + 1
			}
		} else {
			valueStart = privateConfigWordStart(scan, end)
			if render != nil && strings.Contains(s[end:valueStart], "{{") {
				// Skipped comments may themselves supply the private value.
				// Only empty/comment output or one proven @file can be skipped.
				rendered, certain := privateConfigRenderedArgument(s, end, valueStart, render)
				words, err := shlex.Split(rendered)
				if !certain || err != nil {
					return s[:end] + " ***"
				}
				if len(words) > 0 {
					valueRender := func(string) (string, error) { return rendered, nil }
					if privateConfigFileReference(s[end:valueStart], "", "", valueRender) {
						offset = valueStart
						continue
					}
					return s[:end] + " ***"
				}
			}
			if valueStart == len(s) {
				break
			}
			maskStart, mask = valueStart, "***"
			end, complete = privateConfigArgumentEnd(scan, valueStart, 0)
			valueRender := render
			if render != nil && strings.Contains(s[valueStart:end], "{{") {
				rendered, certain := privateConfigRenderedArgument(s, valueStart, end, render)
				if !certain {
					return s[:maskStart] + mask
				}
				valueRender = func(string) (string, error) { return rendered, nil }
			}
			values, valueErr := shlex.Split(scan[valueStart:end])
			if complete && valueErr == nil && len(values) == 1 && privateConfigFileReference(s[valueStart:end], values[0], "", valueRender) {
				offset = end
				continue
			}
		}
		if !complete {
			return s[:maskStart] + mask
		}
		if valueStart < len(scan) && scan[valueStart] != '\'' && scan[valueStart] != '"' {
			// Executed argv is also recorded as joined text, so an unquoted
			// JSON object may contain spaces that are inside this value.
			decoder := json.NewDecoder(strings.NewReader(scan[valueStart:]))
			decoder.UseNumber()
			var value any
			if decoder.Decode(&value) == nil && valueStart+int(decoder.InputOffset()) > end {
				end, complete = privateConfigArgumentEnd(scan, valueStart+int(decoder.InputOffset()), 0)
			}
		}
		if !complete {
			return s[:maskStart] + mask
		}
		s = s[:maskStart] + mask + s[end:]
		scan = s
		if render != nil {
			scan = privateConfigTemplateSource(s)
		}
		offset = maskStart + len(mask)
	}
	return s
}

// Resolve one complete authored word or comment in its variable/control context.
// Repeated or ambiguous output cannot establish a safe private-value boundary.
func privateConfigRenderedArgument(source string, start, end int, render func(string) (string, error)) (string, bool) {
	plain, err := render(source)
	if err != nil {
		return "", false
	}
	begin, finish := "__ECCTL_PRIVATE_BEGIN__", "__ECCTL_PRIVATE_END__"
	for strings.Contains(source, begin) || strings.Contains(source, finish) || strings.Contains(plain, begin) || strings.Contains(plain, finish) {
		begin += begin
		finish += finish
	}
	marked, err := render(source[:start] + begin + source[start:end] + finish + source[end:])
	if err != nil {
		return "", false
	}
	begins, finishes := strings.Count(marked, begin), strings.Count(marked, finish)
	if begins == 0 && finishes == 0 {
		return "", true
	}
	if begins != 1 || finishes != 1 {
		return "", false
	}
	left, right := strings.Index(marked, begin)+len(begin), strings.Index(marked, finish)
	if right < left {
		return "", false
	}
	spanStart, spanEnd := left-len(begin), right-len(begin)
	if marked[:spanStart]+marked[left:right]+marked[right+len(finish):] != plain {
		return "", false
	}
	// A template can emit a quote or backslash that joins the next authored
	// word. Prove both ends against the real rendered shell words before
	// trusting this source interval; later unrelated words need not parse.
	for offset := 0; offset < spanEnd; {
		wordStart := privateConfigWordStart(plain, offset)
		if wordStart >= spanEnd {
			break
		}
		wordEnd, complete := privateConfigArgumentEnd(plain, wordStart, 0)
		if !complete || wordStart < spanStart && wordEnd > spanStart || wordEnd > spanEnd {
			return "", false
		}
		offset = wordEnd
	}
	return marked[left:right], true
}

func privateConfigRenderedKey(rendered string) (string, bool) {
	for offset := 0; offset < len(rendered); {
		start := privateConfigWordStart(rendered, offset)
		if start == len(rendered) {
			break
		}
		end, complete := privateConfigArgumentEnd(rendered, start, 0)
		key := privateConfigInlineKey(rendered[start:end])
		if key == "--runtime-config" || key == "--build-config" {
			return key, true
		}
		words, err := shlex.Split(rendered[start:end])
		if !complete || err != nil {
			break
		}
		if len(words) == 1 {
			key, _, inline := strings.Cut(words[0], "=")
			if key == "--runtime-config" || key == "--build-config" {
				return key, inline || privateConfigWordStart(rendered, end) < len(rendered)
			}
		}
		offset = end
	}
	return "", false
}

// Keep complete Go actions opaque to shell scanning without moving source
// offsets. Quoted strings, raw strings and comments may themselves contain }}.
// Whitespace deleted by Go trim belongs to the same word as the action, so
// marker probes must also encompass it to preserve the rendered boundaries.
func privateConfigTemplateSource(source string) string {
	const space = " \t\r\n"
	view := []byte(source)
	for offset := 0; offset < len(source); {
		opening := strings.Index(source[offset:], "{{")
		if opening < 0 {
			break
		}
		start, end := offset+opening, len(source)
		trimRight := false
		var lexer scanner.Scanner
		lexer.Init(strings.NewReader(source[start+2:]))
		lexer.Mode = scanner.GoTokens
		lexer.Error = func(*scanner.Scanner, string) {}
		for token := lexer.Scan(); token != scanner.EOF; token = lexer.Scan() {
			if lexer.ErrorCount != 0 {
				break
			}
			position := start + 2 + lexer.Position.Offset
			if token == '}' && strings.HasPrefix(source[position:], "}}") {
				end = position + 2
				trimRight = position >= start+4 && source[position-1] == '-' && strings.ContainsRune(space, rune(source[position-2]))
				break
			}
		}
		if start+3 < len(source) && source[start+2] == '-' && strings.ContainsRune(space, rune(source[start+3])) {
			for start > 0 && strings.ContainsRune(space, rune(source[start-1])) {
				start--
			}
		}
		if trimRight {
			for end < len(source) && strings.ContainsRune(space, rune(source[end])) {
				end++
			}
		}
		for index := start; index < end; index++ {
			view[index] = 'x'
		}
		offset = end
	}
	return string(view)
}

func privateConfigFileReference(source, value, prefix string, render func(string) (string, error)) bool {
	if render == nil {
		return strings.HasPrefix(value, "@")
	}
	rendered, err := render(source)
	if err != nil {
		return false
	}
	words, err := shlex.Split(rendered)
	if err != nil {
		return false
	}
	if len(words) == 1 && strings.HasPrefix(words[0], prefix) {
		return strings.HasPrefix(strings.TrimPrefix(words[0], prefix), "@")
	}
	return prefix != "" && len(words) == 2 && words[0] == strings.TrimSuffix(prefix, "=") && strings.HasPrefix(words[1], "@")
}

func privateConfigInlineKey(word string) string {
	equals := strings.IndexByte(word, '=')
	if equals < 0 {
		return ""
	}
	prefix := word[:equals+1]
	var quote byte
	for index := 0; index < len(prefix); index++ {
		ch := prefix[index]
		if ch == '\\' && quote != '\'' {
			index++
		} else if quote != 0 {
			if ch == quote {
				quote = 0
			}
		} else if ch == '\'' || ch == '"' {
			quote = ch
		}
	}
	// Close only a quote begun in the flag prefix; never inspect or repair
	// value text. shlex still validates the exact decoded key and delimiter.
	if quote != 0 {
		prefix += string(quote)
	}
	words, err := shlex.Split(prefix)
	if err == nil && len(words) == 1 {
		key, value, inline := strings.Cut(words[0], "=")
		if inline && value == "" {
			return key
		}
	}
	return ""
}

func privateConfigWordStart(s string, offset int) int {
	for offset < len(s) {
		if strings.ContainsRune(" \t\r\n", rune(s[offset])) {
			offset++
		} else if s[offset] == '#' {
			for offset < len(s) && s[offset] != '\n' {
				offset++
			}
		} else {
			break
		}
	}
	return offset
}

func privateConfigArgumentEnd(s string, start int, quote byte) (int, bool) {
	for index := start; index < len(s); index++ {
		ch := s[index]
		if ch == '\\' && quote != '\'' {
			if index+1 == len(s) {
				return len(s), false
			}
			index++
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
		} else if strings.ContainsRune(" \t\r\n", rune(ch)) {
			return index, true
		}
	}
	return len(s), quote == 0
}

func scrubStructuredJSON(s string) (string, bool) {
	decoder := json.NewDecoder(bytes.NewBufferString(s))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", false
	}
	value = redactJSONValue(value, nil)
	data, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func redactJSONValue(value any, parents []string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := normalizeSensitiveKey(key)
			if isSensitiveKey(normalized) || (normalized == "config" && containsKey(parents, "kubeconfig")) {
				typed[key] = "***"
				continue
			}
			typed[key] = redactJSONValue(child, append(parents, normalized))
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = redactJSONValue(typed[i], parents)
		}
		return typed
	case string:
		return scrubText(typed)
	default:
		return value
	}
}

func normalizeSensitiveKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", ".", "", " ", "")
	return strings.ToLower(replacer.Replace(key))
}

func isSensitiveKey(key string) bool {
	switch key {
	case "accesskeyid", "accesskeysecret", "securitytoken", "password", "token", "secret",
		"clientkeydata", "clientcertificatedata", "certificateauthoritydata":
		return true
	default:
		return false
	}
}

func containsKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

// Redact scrubs sensitive material in place. Call before writing reports.
func (r *Run) Redact() {
	redactCases(r.Cases)
	redactManifest(r.Manifest)
	for i := range r.RegionAttempts {
		r.RegionAttempts[i].Reason = scrub(r.RegionAttempts[i].Reason)
	}
	for i := range r.Executions {
		execution := &r.Executions[i]
		execution.Error = scrub(execution.Error)
		redactCases(execution.Cases)
		redactManifest(execution.Manifest)
		for j := range execution.Attempts {
			execution.Attempts[j].Reason = scrub(execution.Attempts[j].Reason)
		}
	}
}

func redactCases(cases []Case) {
	for i := range cases {
		cases[i].Error = scrub(cases[i].Error)
		for j := range cases[i].Steps {
			st := &cases[i].Steps[j]
			st.Command = scrub(st.Command)
			st.Stdout = scrub(st.Stdout)
			st.Stderr = scrub(st.Stderr)
			st.Error = scrub(st.Error)
			for k := range st.Checks {
				st.Checks[k].Detail = scrub(st.Checks[k].Detail)
			}
		}
	}
}

func redactManifest(manifest []Resource) {
	for i := range manifest {
		manifest[i].Teardown = scrub(manifest[i].Teardown)
	}
}
