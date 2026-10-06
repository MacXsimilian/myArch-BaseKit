package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type textEdit struct {
	start, end int
	value      string
}

func applyTextEdits(text string, edits []textEdit) (string, error) {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end > edits[j].end
		}
		return edits[i].start > edits[j].start
	})
	last := len(text)
	for _, e := range edits {
		if e.start < 0 || e.end < e.start || e.end > last {
			return "", fmt.Errorf("overlapping source edits")
		}
		text = text[:e.start] + e.value + text[e.end:]
		last = e.start
	}
	return text, nil
}

// stripJSONC preserves byte offsets so comments and unrelated properties survive.
func stripJSONC(text string) (string, error) {
	clean := []byte(text)
	if strings.HasPrefix(text, "\xef\xbb\xbf") {
		clean[0], clean[1], clean[2] = ' ', ' ', ' '
	}
	for i := 0; i < len(text); {
		switch {
		case text[i] == '"':
			i++
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == '"' {
					i++
					break
				}
				i++
			}
		case strings.HasPrefix(text[i:], "//"):
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				end = len(text) - i
			}
			for stop := i + end; i < stop; i++ {
				clean[i] = ' '
			}
		case strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return "", fmt.Errorf("unterminated JSONC comment")
			}
			for stop := i + end + 4; i < stop; i++ {
				if clean[i] != '\r' && clean[i] != '\n' {
					clean[i] = ' '
				}
			}
		default:
			i++
		}
	}
	return string(clean), nil
}

func jsonValueAt(text string, at int) (any, int, error) {
	d := json.NewDecoder(strings.NewReader(text[at:]))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, at, err
	}
	return value, at + int(d.InputOffset()), nil
}

func jsonSkip(text string, at int) int {
	for at < len(text) && strings.ContainsRune(" \t\r\n", rune(text[at])) {
		at++
	}
	return at
}

// EditJSONC edits selected top-level properties without evaluating the document.
// Duplicate selected keys are rejected rather than guessing which one is active.
func EditJSONC(text string, desired map[string]any) (string, error) {
	if strings.TrimSpace(text) == "" {
		data, err := json.MarshalIndent(desired, "", "  ")
		return string(data) + "\n", err
	}
	uncommented, err := stripJSONC(text)
	if err != nil {
		return "", err
	}
	chars := []byte(uncommented)
	for i := 0; i < len(chars); {
		if chars[i] == '"' {
			_, next, err := jsonValueAt(uncommented, i)
			if err != nil {
				return "", err
			}
			i = next
		} else if chars[i] == ',' {
			next := jsonSkip(uncommented, i+1)
			if next < len(chars) && (chars[next] == '}' || chars[next] == ']') {
				chars[i] = ' '
			}
			i++
		} else {
			i++
		}
	}
	clean := string(chars)
	d := json.NewDecoder(strings.NewReader(clean))
	d.UseNumber()
	var parsed map[string]any
	if err = d.Decode(&parsed); err != nil {
		return "", fmt.Errorf("settings must be a JSON object: %w", err)
	}
	if parsed == nil {
		return "", fmt.Errorf("settings must be a JSON object")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("unexpected data after settings object")
	}
	index := jsonSkip(clean, 0)
	if index == len(clean) || clean[index] != '{' {
		return "", fmt.Errorf("settings must be a JSON object")
	}
	index++
	properties := map[string][2]int{}
	lastEnd, firstKey, closing := -1, -1, -1
	for {
		index = jsonSkip(clean, index)
		if index >= len(clean) {
			return "", fmt.Errorf("unclosed JSON object")
		}
		if clean[index] == '}' {
			closing = index
			break
		}
		keyStart := index
		keyValue, next, err := jsonValueAt(clean, index)
		if err != nil {
			return "", err
		}
		key, ok := keyValue.(string)
		if !ok {
			return "", fmt.Errorf("property key must be a string")
		}
		index = jsonSkip(clean, next)
		if index >= len(clean) || clean[index] != ':' {
			return "", fmt.Errorf("invalid property separator")
		}
		start := jsonSkip(clean, index+1)
		_, end, err := jsonValueAt(clean, start)
		if err != nil {
			return "", err
		}
		if _, selected := desired[key]; selected {
			if _, duplicate := properties[key]; duplicate {
				return "", fmt.Errorf("duplicate top-level property %s", key)
			}
		}
		properties[key] = [2]int{start, end}
		lastEnd = end
		if firstKey < 0 {
			firstKey = keyStart
		}
		index = jsonSkip(clean, end)
		if index < len(clean) && clean[index] == ',' {
			index++
		} else if index >= len(clean) || clean[index] != '}' {
			return "", fmt.Errorf("invalid property delimiter")
		}
	}
	keys := sortedMapKeys(desired)
	edits := []textEdit{}
	missing := []string{}
	for _, key := range keys {
		encoded, err := json.Marshal(desired[key])
		if err != nil {
			return "", err
		}
		if where, ok := properties[key]; ok {
			current, _ := json.Marshal(parsed[key])
			if string(current) != string(encoded) {
				edits = append(edits, textEdit{where[0], where[1], string(encoded)})
			}
		} else {
			encodedKey, _ := json.Marshal(key)
			missing = append(missing, string(encodedKey)+": "+string(encoded))
		}
	}
	if len(missing) > 0 {
		newline, indent := sourceNewline(text), "  "
		if firstKey >= 0 {
			candidate := text[strings.LastIndex(text[:firstKey], "\n")+1 : firstKey]
			if candidate != "" && strings.TrimSpace(candidate) == "" {
				indent = candidate
			}
		}
		insert := closing
		for insert > 0 && (text[insert-1] == ' ' || text[insert-1] == '\t') {
			insert--
		}
		addition := ""
		if insert == 0 || text[insert-1] != '\n' {
			addition = newline
		}
		addition += indent + strings.Join(missing, ","+newline+indent) + newline
		if lastEnd >= 0 && !strings.Contains(uncommented[lastEnd:closing], ",") {
			if insert == lastEnd {
				addition = "," + addition
			} else {
				edits = append(edits, textEdit{lastEnd, lastEnd, ","})
			}
		}
		edits = append(edits, textEdit{insert, insert, addition})
	}
	return applyTextEdits(text, edits)
}

func sortedMapKeys[V any](m map[string]V) []string {
	result := make([]string, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
func sourceNewline(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func sourceLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func mimeHeader(line string) (string, bool) {
	trim := strings.TrimSpace(line)
	if len(trim) > 2 && trim[0] == '[' && trim[len(trim)-1] == ']' {
		return trim[1 : len(trim)-1], true
	}
	return "", false
}

func mimeLine(line string) (key, before, value, ending string, ok bool) {
	body := strings.TrimRight(line, "\r\n")
	ending = line[len(body):]
	position := strings.IndexByte(body, '=')
	if position < 0 {
		return
	}
	key = strings.TrimSpace(body[:position])
	if key == "" || strings.HasPrefix(key, "#") || strings.HasPrefix(key, ";") {
		return
	}
	start := position + 1
	for start < len(body) && (body[start] == ' ' || body[start] == '\t') {
		start++
	}
	return key, body[:start], body[start:], ending, true
}

func desktopIDs(value string, exclude string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ";") {
		item = strings.TrimSpace(item)
		if item != "" && item != exclude {
			result = append(result, item)
		}
	}
	return result
}

func editMIMESection(text string, intended map[string]string, defaults bool) string {
	if len(intended) == 0 {
		return text
	}
	newline := sourceNewline(text)
	wanted := "Added Associations"
	if defaults {
		wanted = "Default Applications"
	}
	result := []string{}
	section := ""
	found, insertion := false, -1
	seen := map[string]bool{}
	for _, line := range sourceLines(text) {
		if header, ok := mimeHeader(line); ok {
			if section == wanted && insertion < 0 {
				insertion = len(result)
			}
			section = header
			found = found || section == wanted
		} else if section == wanted || (!defaults && section == "Removed Associations") {
			key, before, prior, ending, ok := mimeLine(line)
			if desktop, selected := intended[key]; ok && selected {
				values := desktopIDs(prior, desktop)
				if section == wanted {
					values = append([]string{desktop}, values...)
					seen[key] = true
				}
				value := strings.Join(values, ";")
				if len(values) > 0 {
					value += ";"
				}
				line = before + value + ending
			}
		}
		result = append(result, line)
	}
	if !found {
		if len(result) > 0 && !strings.HasSuffix(result[len(result)-1], "\n") {
			result[len(result)-1] += newline
		}
		result = append(result, "["+wanted+"]"+newline)
		insertion = len(result)
	} else if insertion < 0 {
		insertion = len(result)
	}
	missing := []string{}
	for _, key := range sortedMapKeys(intended) {
		if !seen[key] {
			missing = append(missing, key+"="+intended[key]+";"+newline)
		}
	}
	if len(missing) > 0 {
		if insertion > 0 && !strings.HasSuffix(result[insertion-1], "\n") {
			result[insertion-1] += newline
		}
		combined := make([]string, 0, len(result)+len(missing))
		combined = append(combined, result[:insertion]...)
		combined = append(combined, missing...)
		combined = append(combined, result[insertion:]...)
		result = combined
	}
	return strings.Join(result, "")
}

func EditMIMEAssociations(text string, intended map[string]string) string {
	return editMIMESection(text, intended, false)
}
func EditMIMEDefaults(text string, intended map[string]string) string {
	return editMIMESection(text, intended, true)
}
