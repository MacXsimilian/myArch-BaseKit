package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

// These readers inspect literal schema defaults without evaluating upstream
// JavaScript. Unknown expressions fail closed instead of inferring a type.
func dmsUncomment(source string) string {
	clean := []byte(source)
	for i := 0; i < len(source); {
		if source[i] == '\'' || source[i] == '"' || source[i] == '`' {
			quote := source[i]
			i++
			for i < len(source) {
				if source[i] == '\\' {
					i += 2
				} else if source[i] == quote {
					i++
					break
				} else {
					i++
				}
			}
			continue
		}
		end := i
		if strings.HasPrefix(source[i:], "//") {
			end = strings.IndexByte(source[i:], '\n')
			if end < 0 {
				end = len(source)
			} else {
				end += i
			}
		} else if strings.HasPrefix(source[i:], "/*") {
			end = strings.Index(source[i+2:], "*/")
			if end < 0 {
				return ""
			}
			end += i + 4
		}
		if end > i {
			for ; i < end; i++ {
				if clean[i] != '\n' && clean[i] != '\r' {
					clean[i] = ' '
				}
			}
		} else {
			i++
		}
	}
	return string(clean)
}

// dmsField reads a direct object member, skipping nested values and strings.
// The outer braces are optional for the body returned by a schema declaration.
func dmsField(object, key string) string {
	object = strings.TrimSpace(object)
	if strings.HasPrefix(object, "{") {
		object = object[1:]
	}
	for at := 0; at < len(object); {
		at = jsonSkip(object, at)
		if at >= len(object) || object[at] == '}' {
			break
		}
		match := regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*\s*:`).FindString(object[at:])
		if match == "" {
			return ""
		}
		name := strings.TrimSpace(strings.TrimSuffix(match, ":"))
		at = jsonSkip(object, at+len(match))
		value, end := dmsLiteral(object, at)
		if value == "" {
			return ""
		}
		if name == key {
			return value
		}
		at = jsonSkip(object, end)
		if at < len(object) && object[at] == ',' {
			at++
		} else {
			break
		}
	}
	return ""
}

func dmsLiteral(source string, start int) (string, int) {
	depth, quote := 0, byte(0)
	for i := start; i < len(source); i++ {
		ch := source[i]
		if quote != 0 {
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
		} else if ch == '{' || ch == '[' || ch == '(' {
			depth++
		} else if ch == '}' || ch == ']' || ch == ')' {
			if depth == 0 {
				return strings.TrimSpace(source[start:i]), i
			}
			depth--
		} else if ch == ',' && depth == 0 {
			return strings.TrimSpace(source[start:i]), i
		}
	}
	if depth == 0 && quote == 0 {
		return strings.TrimSpace(source[start:]), len(source)
	}
	return "", start
}

func dmsValueType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch value[0] {
	case '{':
		if dmsCompleteContainer(value) {
			return "object"
		}
	case '[':
		if dmsCompleteContainer(value) {
			return "array"
		}
	case '"', '\'':
		if regexp.MustCompile(`^(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')$`).MatchString(value) {
			return "string"
		}
	}
	if value == "true" || value == "false" {
		return "boolean"
	}
	if regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`).MatchString(value) {
		return "number"
	}
	return ""
}

func dmsCompleteContainer(value string) bool {
	stack := []byte{}
	quote := byte(0)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '{', '[', '(':
			stack = append(stack, ch)
		case '}', ']', ')':
			if len(stack) == 0 {
				return false
			}
			opening := stack[len(stack)-1]
			if (ch == '}' && opening != '{') || (ch == ']' && opening != '[') || (ch == ')' && opening != '(') {
				return false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i == len(value)-1
			}
		}
	}
	return false
}

func dmsDefault(source, key string) string {
	// LOCAL_SPEC takes precedence over Shared SPEC, matching the upstream merge.
	for _, declaration := range regexp.MustCompile(`\bvar\s+(?:LOCAL_SPEC|SPEC)\s*=\s*\{`).FindAllStringIndex(source, -1) {
		if field := dmsField(source[declaration[1]-1:], key); field != "" {
			return dmsField(field, "def")
		}
	}
	return ""
}

func dmsMatchesDefault(source, key string, expected any) bool {
	encoded, err := json.Marshal(expected)
	return err == nil && dmsValueType(dmsDefault(source, key)) == dmsValueType(string(encoded))
}

func dmsMatchesRecord(record string, expected map[string]any) bool {
	for key, value := range expected {
		encoded, err := json.Marshal(value)
		if err != nil || dmsValueType(dmsField(record, key)) != dmsValueType(string(encoded)) {
			return false
		}
	}
	return true
}
