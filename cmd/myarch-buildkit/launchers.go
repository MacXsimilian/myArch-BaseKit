package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

var terminalPrograms = map[string]bool{"kitty": true, "alacritty": true, "foot": true, "footclient": true, "konsole": true, "cosmic-term": true, "wezterm": true, "xterm": true, "xfce4-terminal": true, "gnome-terminal": true, "qterminal": true, "terminator": true, "ghostty": true}
var editorPrograms = map[string]bool{"gnome-text-editor": true, "cosmic-edit": true, "gedit": true, "kate": true, "mousepad": true, "leafpad": true, "xed": true, "code": true, "code-oss": true, "codium": true, "nvim": true, "gvim": true}
var launcherPattern = regexp.MustCompile(`^([ \t]*(?:uwsm[ \t]+app[ \t]+--[ \t]+)?)(/usr/bin/)?([a-zA-Z0-9_-]+)(.*)$`)

type launcherResult struct {
	Role, Replacement, Program string
	Safe                       bool
}

// launcherCommand accepts only understood direct launch forms. Shell wrappers,
// substitution, pipeline syntax and application-specific options stay intact.
func launcherCommand(value string) *launcherResult {
	match := launcherPattern.FindStringSubmatch(value)
	if match == nil {
		return nil
	}
	program, tail := match[3], match[4]
	if tail != "" && !strings.ContainsRune(" \t\r\n;&|`$<>", rune(tail[0])) {
		return nil
	}
	role := ""
	if terminalPrograms[program] {
		role = "terminal"
	} else if editorPrograms[program] {
		role = "editor"
	} else {
		return nil
	}
	result := &launcherResult{Role: role, Program: program}
	if strings.ContainsAny(value, "\r\n;&|`$<>") {
		return result
	}
	for _, r := range value {
		if !unicode.IsPrint(r) && r != '\t' {
			return result
		}
	}
	args, err := shellWords(tail)
	if err != nil {
		return result
	}
	supported := len(args) == 0 || (role == "terminal" && len(args) >= 2 && args[0] == "-e") || (role == "editor" && len(args) == 1 && args[0] == "--wait")
	if !supported {
		return result
	}
	replacement := "ghostty"
	if role == "editor" {
		replacement = "code"
	}
	result.Replacement = match[1] + match[2] + replacement + tail
	result.Safe = true
	return result
}

func shellWords(text string) ([]string, error) {
	words := []string{}
	var word strings.Builder
	quote := byte(0)
	started := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			} else {
				word.WriteByte(ch)
			}
			continue
		}
		if ch == '\\' {
			i++
			if i >= len(text) {
				return nil, fmt.Errorf("unclosed shell escape")
			}
			if quote == '"' && !strings.ContainsRune("\\\"$`", rune(text[i])) {
				word.WriteByte('\\')
			}
			word.WriteByte(text[i])
			started = true
			continue
		}
		if quote == '"' {
			if ch == '"' {
				quote = 0
			} else {
				word.WriteByte(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			started = true
			continue
		}
		if ch == ' ' || ch == '\t' {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		word.WriteByte(ch)
		started = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed shell quote")
	}
	if started {
		words = append(words, word.String())
	}
	return words, nil
}

type luaToken struct {
	Kind, Value               string
	Known                     bool
	Start, End, Braces, Order int
	Blocks                    []string
}

var luaName = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*`)
var luaLong = regexp.MustCompile(`^\[(=*)\[`)

func tokenizeLua(text string) ([]luaToken, string, error) {
	tokens := []luaToken{}
	clean := []byte(text)
	blocks := []string{}
	braces := 0
	newToken := func(kind, value string, known bool, start, end int) {
		tokens = append(tokens, luaToken{Kind: kind, Value: value, Known: known, Start: start, End: end, Braces: braces, Blocks: append([]string{}, blocks...)})
	}
	for at := 0; at < len(text); {
		start, ch := at, text[at]
		if strings.ContainsRune(" \t\r\n\f\v", rune(ch)) {
			at++
			continue
		}
		comment := strings.HasPrefix(text[at:], "--")
		opening := at
		if comment {
			opening += 2
		}
		if long := luaLong.FindStringSubmatch(text[opening:]); long != nil {
			closing := "]" + long[1] + "]"
			end := strings.Index(text[opening+len(long[0]):], closing)
			if end < 0 {
				return nil, "", fmt.Errorf("unterminated Lua long string/comment")
			}
			at = opening + len(long[0]) + end + len(closing)
			if comment {
				for i := start; i < at; i++ {
					if clean[i] != '\r' && clean[i] != '\n' {
						clean[i] = ' '
					}
				}
			} else {
				newToken("long_string", "", false, start, at)
			}
			continue
		}
		if comment {
			end := strings.IndexByte(text[at:], '\n')
			if end < 0 {
				at = len(text)
			} else {
				at += end
			}
			for i := start; i < at; i++ {
				clean[i] = ' '
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			at++
			closed := false
			for at < len(text) {
				if text[at] == '\\' {
					at += 2
				} else if text[at] == ch {
					at++
					closed = true
					break
				} else {
					at++
				}
			}
			if !closed {
				return nil, "", fmt.Errorf("unterminated Lua quoted string")
			}
			value := text[start+1 : at-1]
			known := !strings.ContainsAny(value, "\\\n\r")
			newToken("string", value, known, start, at)
			continue
		}
		if identifier := luaName.FindString(text[at:]); identifier != "" {
			at += len(identifier)
			newToken("name", identifier, true, start, at)
			switch identifier {
			case "function", "if", "repeat":
				blocks = append(blocks, identifier)
			case "for", "while":
				blocks = append(blocks, identifier+"_pending")
			case "do":
				if len(blocks) > 0 && (blocks[len(blocks)-1] == "for_pending" || blocks[len(blocks)-1] == "while_pending") {
					blocks[len(blocks)-1] = strings.TrimSuffix(blocks[len(blocks)-1], "_pending")
				} else {
					blocks = append(blocks, "do")
				}
			case "end":
				if len(blocks) > 0 {
					blocks = blocks[:len(blocks)-1]
				}
			case "until":
				if len(blocks) > 0 && blocks[len(blocks)-1] == "repeat" {
					blocks = blocks[:len(blocks)-1]
				}
			}
			continue
		}
		value := string(ch)
		if strings.HasPrefix(text[at:], "..") {
			value = ".."
		}
		at += len(value)
		newToken("symbol", value, true, start, at)
		if ch == '{' {
			braces++
		} else if ch == '}' && braces > 0 {
			braces--
		}
	}
	return tokens, string(clean), nil
}

func luaCallEnd(tokens []luaToken, opening int) (int, error) {
	depth := 0
	for i := opening; i < len(tokens); i++ {
		if tokens[i].Kind == "symbol" && tokens[i].Value == "(" {
			depth++
		} else if tokens[i].Kind == "symbol" && tokens[i].Value == ")" {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unbalanced Lua call parentheses")
}

type launcherDocument struct {
	Path, Text, Clean string
	Tokens            []luaToken
	Edits             []textEdit
}
type legacyEvent struct {
	Doc    *launcherDocument
	Offset int
	Line   string
}
type launcherSite struct {
	Doc               *launcherDocument
	Name, Value, Role string
	Known, Blocked    bool
	Start, End, Order int
	Uses              []bool
}

type launcherEditor struct {
	Context                    *Context
	Details                    map[string]any
	ConfigureEditor, SafeGraph bool
	Root                       string
	Allowed                    []string
	Documents                  []*launcherDocument
	Loaded                     map[string]*launcherDocument
	Legacy                     []legacyEvent
	Counter                    int
}

func launcherRole(name string) string {
	parts := strings.Split(name, ".")
	name = strings.ReplaceAll(strings.ToLower(parts[len(parts)-1]), "_", "")
	if defaultContainsString([]string{"terminal", "terminalcmd", "terminalcommand"}, name) {
		return "terminal"
	}
	if defaultContainsString([]string{"editor", "texteditor", "codeeditor", "editorcmd", "editorcommand"}, name) {
		return "editor"
	}
	return ""
}

func (e *launcherEditor) caution(message string, graph bool) {
	warnings := stringList(e.Details["warnings"])
	if !defaultContainsString(warnings, message) {
		e.Details["warnings"] = append(warnings, message)
	}
	if graph {
		e.SafeGraph = false
	}
}
func (e *launcherEditor) old(program string) {
	prior := stringList(e.Details["terminal_candidates"])
	if program != "ghostty" && !defaultContainsString(prior, program) {
		e.Details["terminal_candidates"] = append(prior, program)
	}
}
func (e *launcherEditor) allowed(path string) bool {
	for _, root := range e.Allowed {
		if inside(root, path) {
			return true
		}
	}
	return false
}

func canonicalParent(path string) (string, error) {
	path = filepath.Clean(path)
	missing := []string{}
	for {
		canonical, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			return canonical, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = append(missing, filepath.Base(path))
		path = parent
	}
}

func (e *launcherEditor) includes(value, kind string) ([]string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("a dynamic or escaped include path cannot be traced safely")
	}
	if kind == "lua" {
		if !strings.Contains(value, "/") && !strings.HasSuffix(value, ".lua") {
			value = strings.ReplaceAll(value, ".", "/")
		}
		if !strings.HasSuffix(value, ".lua") && !strings.ContainsAny(value, "*?[") {
			value += ".lua"
		}
	}
	value = strings.NewReplacer("${HOME}", e.Context.Home, "$HOME", e.Context.Home, "${XDG_CONFIG_HOME}", e.Context.ConfigHome, "$XDG_CONFIG_HOME", e.Context.ConfigHome).Replace(value)
	if strings.Contains(value, "$") {
		return nil, fmt.Errorf("a variable-based include path was not resolved")
	}
	if value == "~" {
		value = e.Context.Home
	} else if strings.HasPrefix(value, "~/") {
		value = filepath.Join(e.Context.Home, value[2:])
	} else if strings.HasPrefix(value, "~") {
		return nil, fmt.Errorf("another user's include path cannot be traced")
	}
	if !filepath.IsAbs(value) {
		if kind != "lua" {
			return nil, fmt.Errorf("relative legacy source paths require manual review")
		}
		value = filepath.Join(e.Root, value)
	}
	anchor := filepath.Dir(value)
	wildcard := strings.IndexAny(value, "*?[")
	if wildcard >= 0 {
		anchor = filepath.Dir(value[:wildcard])
	}
	canonical, err := canonicalParent(anchor)
	if err != nil {
		return nil, err
	}
	if !e.allowed(canonical) {
		return nil, fmt.Errorf("include outside user configuration roots: %s", value)
	}
	paths := []string{value}
	if wildcard >= 0 {
		paths, err = filepath.Glob(value)
		if err != nil {
			return nil, err
		}
		sort.Strings(paths)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("include matched no files: %s", value)
	}
	return paths, nil
}

var legacySource = regexp.MustCompile(`^[ \t]*source[ \t]*=[ \t]*(.*?)[ \t]*$`)

func (e *launcherEditor) load(path, kind string) error {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !e.allowed(canonical) {
		return fmt.Errorf("include resolves outside user configuration roots: %s", path)
	}
	if _, found := e.Loaded[canonical]; found {
		if kind != "lua" {
			e.caution("Repeated legacy source needs review: "+canonical, true)
		}
		return nil
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return err
	}
	if len(e.Documents) >= 128 || info.Size() > 2*1024*1024 {
		return fmt.Errorf("Hyprland include graph exceeds the conservative size limit")
	}
	if owner, ok := info.Sys().(*syscall.Stat_t); !ok || int(owner.Uid) != os.Getuid() {
		return fmt.Errorf("configuration belongs to another user: %s", canonical)
	}
	text, err := configRead(e.Context, canonical)
	if err != nil {
		return err
	}
	doc := &launcherDocument{Path: canonical, Text: text, Clean: text}
	if kind == "lua" {
		doc.Tokens, doc.Clean, err = tokenizeLua(text)
		if err != nil {
			return err
		}
	}
	e.Loaded[canonical] = doc
	e.Documents = append(e.Documents, doc)
	e.Details["loaded_paths"] = append(stringList(e.Details["loaded_paths"]), canonical)
	if kind == "lua" {
		for i := range doc.Tokens {
			e.Counter++
			doc.Tokens[i].Order = e.Counter
			token := doc.Tokens[i]
			if token.Kind != "name" || token.Value != "require" || (i > 0 && (doc.Tokens[i-1].Value == "." || doc.Tokens[i-1].Value == ":")) {
				continue
			}
			var literal *luaToken
			if i+3 < len(doc.Tokens) && doc.Tokens[i+1].Value == "(" && doc.Tokens[i+2].Kind == "string" && doc.Tokens[i+3].Value == ")" {
				literal = &doc.Tokens[i+2]
			} else if i+1 < len(doc.Tokens) && doc.Tokens[i+1].Kind == "string" {
				literal = &doc.Tokens[i+1]
			}
			if len(token.Blocks) > 0 || literal == nil || !literal.Known {
				e.caution("Dynamic or conditional require in "+canonical+"; additional launcher code may be loaded", true)
				continue
			}
			paths, err := e.includes(literal.Value, kind)
			if err != nil {
				e.caution(canonical+": "+err.Error(), true)
				continue
			}
			for _, child := range paths {
				if err = e.load(child, kind); err != nil {
					e.caution(canonical+": "+err.Error(), true)
				}
			}
		}
	} else {
		offset := 0
		for _, line := range sourceLines(text) {
			clean := strings.TrimRight(strings.SplitN(line, "#", 2)[0], "\r\n")
			if match := legacySource.FindStringSubmatch(clean); match != nil {
				paths, err := e.includes(match[1], kind)
				if err != nil {
					e.caution(canonical+": "+err.Error(), true)
				} else {
					for _, child := range paths {
						if err = e.load(child, kind); err != nil {
							e.caution(canonical+": "+err.Error(), true)
						}
					}
				}
			} else {
				e.Legacy = append(e.Legacy, legacyEvent{doc, offset, clean})
			}
			offset += len(line)
		}
	}
	return nil
}

func (e *launcherEditor) edit(doc *launcherDocument, start, end int, value string) {
	if doc.Text[start:end] != value {
		doc.Edits = append(doc.Edits, textEdit{start, end, value})
	}
}
func quoteLua(value string) string { data, _ := json.Marshal(value); return string(data) }

var luaAssignment = regexp.MustCompile(`(?m)^[ \t]*(local[ \t]+)?([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)[ \t]*=[ \t]*`)

func luaDefinitionKey(scope, name string) string { return scope + "\x00" + name }

func luaExpression(doc *launcherDocument, tokens []luaToken, order int, definitions map[string][]*launcherSite) (string, []*launcherSite, bool) {
	pieces := []string{}
	refs := []*launcherSite{}
	wantValue := true
	for index := 0; index < len(tokens); {
		token := tokens[index]
		if !wantValue {
			if token.Value != ".." {
				return "", refs, false
			}
			wantValue = true
			index++
			continue
		}
		if token.Kind == "string" && token.Known {
			pieces = append(pieces, token.Value)
			index++
		} else if token.Kind == "name" {
			name := token.Value
			index++
			for index+1 < len(tokens) && tokens[index].Value == "." && tokens[index+1].Kind == "name" {
				name += "." + tokens[index+1].Value
				index += 2
			}
			sites, ok := definitions[luaDefinitionKey(doc.Path, name)]
			if !ok {
				sites = definitions[luaDefinitionKey("", name)]
			}
			if len(sites) != 1 {
				return "", refs, false
			}
			site := sites[0]
			refs = append(refs, site)
			if !site.Known || site.Order >= order {
				return "", refs, false
			}
			pieces = append(pieces, site.Value)
		} else {
			return "", refs, false
		}
		wantValue = false
	}
	return strings.Join(pieces, ""), refs, !wantValue
}

func tokenPrefix(tokens []luaToken, at int, values ...string) bool {
	if at+len(values) > len(tokens) {
		return false
	}
	for i, value := range values {
		if tokens[at+i].Value != value {
			return false
		}
	}
	return true
}

func (e *launcherEditor) lua() error {
	definitions := map[string][]*launcherSite{}
	for _, doc := range e.Documents {
		byStart := map[int]*luaToken{}
		for i := range doc.Tokens {
			byStart[doc.Tokens[i].Start] = &doc.Tokens[i]
		}
		handled := map[int]bool{}
		for _, match := range luaAssignment.FindAllStringSubmatchIndex(doc.Clean, -1) {
			firstAt := match[0]
			for firstAt < match[1] && (doc.Clean[firstAt] == ' ' || doc.Clean[firstAt] == '\t') {
				firstAt++
			}
			first := byStart[firstAt]
			if first == nil {
				continue
			}
			name := doc.Clean[match[4]:match[5]]
			role := launcherRole(name)
			if len(first.Blocks) > 0 || first.Braces > 0 {
				if role != "" {
					e.caution("Scoped or conditional "+role+" assignment in "+doc.Path+"; launchers require manual review", true)
				}
				continue
			}
			rhs := byStart[match[1]]
			lineEnd := strings.IndexByte(doc.Clean[match[1]:], '\n')
			if lineEnd < 0 {
				lineEnd = len(doc.Clean)
			} else {
				lineEnd += match[1]
			}
			literal := rhs != nil && rhs.Kind == "string" && rhs.End <= lineEnd && regexp.MustCompile(`^[ \t\r]*;?[ \t\r]*$`).MatchString(doc.Clean[rhs.End:lineEnd])
			scope := ""
			if match[2] >= 0 {
				scope = doc.Path
			}
			parts := strings.Split(name, ".")
			handled[match[5]-len(parts[len(parts)-1])] = true
			site := &launcherSite{Doc: doc, Name: name, Role: role, Order: first.Order}
			if literal {
				site.Value = rhs.Value
				site.Known = rhs.Known
				site.Start = rhs.Start
				site.End = rhs.End
			}
			key := luaDefinitionKey(scope, name)
			definitions[key] = append(definitions[key], site)
		}
		for i, token := range doc.Tokens {
			if token.Kind == "name" && launcherRole(token.Value) != "" && i+1 < len(doc.Tokens) && doc.Tokens[i+1].Value == "=" && (i+2 >= len(doc.Tokens) || doc.Tokens[i+2].Value != "=") && !handled[token.Start] {
				e.caution("Unresolved scoped or inline launcher assignment in "+doc.Path+"; configuration preserved", true)
			}
		}
	}
	verified := map[string]int{"terminal": 0, "editor": 0}
	unresolved := map[string]bool{"terminal": false, "editor": false}
	for _, doc := range e.Documents {
		tokens := doc.Tokens
		bindRanges := [][2]int{}
		for i := range tokens {
			if tokenPrefix(tokens, i, "hl", ".", "unbind") {
				e.caution("Dynamic keybinding removal in "+doc.Path+"; launcher state requires manual review", true)
			}
			if tokenPrefix(tokens, i, "hl", ".", "bind", "(") && len(tokens[i].Blocks) == 0 {
				end, err := luaCallEnd(tokens, i+3)
				if err != nil {
					return err
				}
				bindRanges = append(bindRanges, [2]int{i, end})
			}
		}
		for i := range tokens {
			opening := -1
			if tokenPrefix(tokens, i, "hl", ".", "dsp", ".", "exec_cmd", "(") {
				opening = i + 5
			} else if tokenPrefix(tokens, i, "hl", ".", "exec_cmd", "(") {
				opening = i + 3
			}
			if opening < 0 {
				continue
			}
			end, err := luaCallEnd(tokens, opening)
			if err != nil {
				return err
			}
			items := tokens[opening+1 : end]
			isBind := false
			for _, where := range bindRanges {
				isBind = isBind || (where[0] < i && i < where[1])
			}
			value, refs, known := luaExpression(doc, items, tokens[i].Order, definitions)
			var command *launcherResult
			if known {
				command = launcherCommand(value)
			}
			if command == nil {
				for _, site := range refs {
					if site.Role != "" {
						site.Blocked = true
						unresolved[site.Role] = true
					}
				}
				continue
			}
			role := command.Role
			if role == "editor" && !e.ConfigureEditor {
				continue
			}
			if role == "terminal" {
				e.old(command.Program)
			}
			if !command.Safe {
				unresolved[role] = true
				for _, site := range refs {
					site.Blocked = true
				}
				e.caution("Unsupported "+role+" launch options in "+doc.Path+"; the command was preserved", false)
				continue
			}
			candidates := []*launcherSite{}
			for _, site := range refs {
				if site.Role == role && launcherCommand(site.Value) != nil {
					candidates = append(candidates, site)
				}
			}
			if len(refs) == 0 && len(items) == 1 && items[0].Kind == "string" {
				e.edit(doc, items[0].Start, items[0].End, quoteLua(command.Replacement))
				if isBind {
					verified[role]++
				}
			} else if len(candidates) == 1 {
				candidates[0].Uses = append(candidates[0].Uses, isBind)
			} else {
				unresolved[role] = true
			}
		}
	}
	for _, sites := range definitions {
		if len(sites) != 1 {
			for _, site := range sites {
				if site.Role != "" {
					unresolved[site.Role] = true
				}
			}
			continue
		}
		site := sites[0]
		if len(site.Uses) == 0 || site.Blocked {
			continue
		}
		command := launcherCommand(site.Value)
		if command == nil || !command.Safe {
			unresolved[site.Role] = true
			continue
		}
		e.edit(site.Doc, site.Start, site.End, quoteLua(command.Replacement))
		for _, isBind := range site.Uses {
			if isBind {
				verified[command.Role]++
			}
		}
	}
	e.Details["launcher_verified"] = verified["terminal"] > 0 && !unresolved["terminal"] && e.SafeGraph
	e.Details["editor_verified"] = verified["editor"] > 0 && !unresolved["editor"] && e.SafeGraph
	return nil
}

var legacyAssignment = regexp.MustCompile(`^[ \t]*\$([A-Za-z_]\w*)[ \t]*=[ \t]*(.*?)[ \t]*$`)
var legacyBind = regexp.MustCompile(`^[ \t]*(bind[a-z]*)[ \t]*=[ \t]*(.*)$`)
var legacyVariable = regexp.MustCompile(`\$([A-Za-z_]\w*)`)
var legacyUnsafe = regexp.MustCompile(`^[ \t]*(?:unbind[ \t]*=|submap[ \t]*=)`)

func (e *launcherEditor) legacy() {
	values := map[string]string{}
	sites := map[string]*launcherSite{}
	assignments := []*launcherSite{}
	verified := map[string]int{"terminal": 0, "editor": 0}
	unresolved := map[string]bool{"terminal": false, "editor": false}
	for _, event := range e.Legacy {
		if legacyUnsafe.MatchString(event.Line) {
			e.caution("Submap or unbind directives in "+event.Doc.Path+"; launcher state requires manual review", true)
		}
		if match := legacyAssignment.FindStringSubmatchIndex(event.Line); match != nil {
			name, value := event.Line[match[2]:match[3]], event.Line[match[4]:match[5]]
			site := &launcherSite{Doc: event.Doc, Name: name, Value: value, Known: true, Role: launcherRole(name), Start: event.Offset + match[4], End: event.Offset + match[5]}
			values[name], sites[name] = value, site
			assignments = append(assignments, site)
			continue
		}
		match := legacyBind.FindStringSubmatchIndex(event.Line)
		if match == nil {
			continue
		}
		kind, body := event.Line[match[2]:match[3]], event.Line[match[4]:match[5]]
		count := 3
		if strings.Contains(kind[4:], "d") {
			count = 4
		}
		parts := strings.SplitN(body, ",", count+1)
		if len(parts) != count+1 || !defaultContainsString([]string{"exec", "execr"}, strings.TrimSpace(parts[len(parts)-2])) {
			continue
		}
		raw := parts[len(parts)-1]
		refs := []*launcherSite{}
		references := legacyVariable.FindAllStringSubmatch(raw, -1)
		missing := false
		for _, ref := range references {
			if site, ok := sites[ref[1]]; ok {
				refs = append(refs, site)
			} else {
				missing = true
			}
		}
		expanded := legacyVariable.ReplaceAllStringFunc(raw, func(ref string) string {
			if value, ok := values[ref[1:]]; ok {
				return value
			}
			return ref
		})
		var command *launcherResult
		if !missing {
			command = launcherCommand(expanded)
		}
		if command == nil {
			for _, site := range refs {
				if site.Role != "" {
					site.Blocked = true
					unresolved[site.Role] = true
				}
			}
			continue
		}
		role := command.Role
		if role == "editor" && !e.ConfigureEditor {
			continue
		}
		if role == "terminal" {
			e.old(command.Program)
		}
		if !command.Safe {
			unresolved[role] = true
			for _, site := range refs {
				site.Blocked = true
			}
			e.caution("Unsupported "+role+" launch options in "+event.Doc.Path+"; the command was preserved", false)
		} else if len(references) == 0 {
			start := event.Offset + match[4]
			for _, part := range parts[:len(parts)-1] {
				start += len(part) + 1
			}
			e.edit(event.Doc, start, start+len(raw), command.Replacement)
			verified[role]++
		} else {
			candidates := []*launcherSite{}
			for _, site := range refs {
				if site.Role == role {
					candidates = append(candidates, site)
				}
			}
			if len(candidates) == 1 {
				candidates[0].Uses = append(candidates[0].Uses, true)
			} else {
				unresolved[role] = true
			}
		}
	}
	for _, site := range assignments {
		if len(site.Uses) == 0 || site.Blocked {
			continue
		}
		command := launcherCommand(site.Value)
		if command == nil || !command.Safe {
			unresolved[site.Role] = true
			continue
		}
		e.edit(site.Doc, site.Start, site.End, command.Replacement)
		verified[command.Role] += len(site.Uses)
	}
	e.Details["launcher_verified"] = verified["terminal"] > 0 && !unresolved["terminal"] && e.SafeGraph
	e.Details["editor_verified"] = verified["editor"] > 0 && !unresolved["editor"] && e.SafeGraph
}

// EditHyprlandLaunchers follows only static, owned user includes and refuses
// the entire graph when dynamic includes or scoped launcher mutations appear.
// It never evaluates Lua or shell configuration.
func EditHyprlandLaunchers(c *Context, path string, configureEditor bool) (map[string]any, error) {
	details := map[string]any{"config_path": path, "format": "hyprlang", "loaded_paths": []string{}, "changed_paths": []string{}, "launcher_verified": false, "editor_verified": false, "terminal_candidates": []string{}, "warnings": []string{}}
	root, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return details, err
	}
	home, err := filepath.EvalSymlinks(c.Home)
	if err != nil {
		return details, err
	}
	kind := "hyprlang"
	if filepath.Ext(path) == ".lua" {
		kind = "lua"
		details["format"] = kind
	}
	editor := &launcherEditor{Context: c, Details: details, ConfigureEditor: configureEditor, SafeGraph: true, Root: root, Allowed: []string{home, root}, Loaded: map[string]*launcherDocument{}}
	if err = editor.load(path, kind); err != nil {
		return details, err
	}
	if kind == "lua" {
		err = editor.lua()
	} else {
		editor.legacy()
	}
	if err != nil {
		return details, err
	}
	if !editor.SafeGraph {
		details["launcher_verified"], details["editor_verified"] = false, false
		editor.caution("The loaded configuration could not be fully traced; all Hyprland configuration files were preserved", false)
		return details, nil
	}
	for _, doc := range editor.Documents {
		if len(doc.Edits) == 0 {
			continue
		}
		text, err := applyTextEdits(doc.Text, doc.Edits)
		if err != nil {
			return details, err
		}
		if err = c.Write(doc.Path, []byte(text), 0600); err != nil {
			return details, err
		}
		details["changed_paths"] = append(stringList(details["changed_paths"]), doc.Path)
	}
	if verified, _ := details["editor_verified"].(bool); !verified {
		editor.caution("The editor shortcut was not verified; set its command to code in the active configuration if needed", false)
	}
	return details, nil
}
