package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const shellBlockBegin = "# >>> cachyos-tooling-bootstrap >>>"
const shellBlockEnd = "# <<< cachyos-tooling-bootstrap <<<"
const composeProvider = "/usr/bin/podman-compose"

var shellTools = []string{"mise", "zoxide", "fzf", "starship", "direnv"}
var editorVars = []string{"EDITOR", "VISUAL", "GIT_EDITOR", "GH_EDITOR"}

// Removing our complete blocks never evaluates, reformats or deletes user text.
func withoutShellBlock(text, label string) (string, error) {
	var kept strings.Builder
	inside := false
	for _, line := range strings.SplitAfter(text, "\n") {
		switch strings.TrimSpace(line) {
		case shellBlockBegin:
			if inside {
				return "", fmt.Errorf("nested managed block in %s; file unchanged", label)
			}
			inside = true
		case shellBlockEnd:
			if !inside {
				return "", fmt.Errorf("unmatched managed block end in %s; file unchanged", label)
			}
			inside = false
		default:
			if !inside {
				kept.WriteString(line)
			}
		}
	}
	if inside {
		return "", fmt.Errorf("unclosed managed block in %s; file unchanged", label)
	}
	return kept.String(), nil
}
func withShellBlock(base, body string) string {
	separator := ""
	if base != "" && !strings.HasSuffix(base, "\n\n") {
		if strings.HasSuffix(base, "\n") {
			separator = "\n"
		} else {
			separator = "\n\n"
		}
	}
	return base + separator + shellBlockBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + shellBlockEnd + "\n"
}
func shellText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// This lexer is only an inspection aid: no expansion or executable shell parsing.
func inspectShellWords(line string) ([]string, error) {
	words := []string{}
	var token strings.Builder
	quote := byte(0)
	escaped := false
	have := false
	flush := func() {
		if have {
			words = append(words, token.String())
			token.Reset()
			have = false
		}
	}
	for i := 0; i < len(line); i++ {
		b := line[i]
		if escaped {
			token.WriteByte(b)
			have = true
			escaped = false
			continue
		}
		if b == '\\' && quote != '\'' {
			escaped = true
			have = true
			continue
		}
		if quote != 0 {
			if b == quote {
				quote = 0
			} else {
				token.WriteByte(b)
			}
			have = true
			continue
		}
		if b == '\'' || b == '"' {
			quote = b
			have = true
			continue
		}
		if b == '#' && !have {
			break
		}
		if b == ' ' || b == '\t' {
			flush()
			continue
		}
		token.WriteByte(b)
		have = true
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("incomplete quoted shell line")
	}
	flush()
	return words, nil
}
func activeShellText(text string) string {
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if regexp.MustCompile(`^(?:echo|printf)\s`).MatchString(trimmed) {
			continue
		}
		words, err := inspectShellWords(line)
		if err == nil {
			line = strings.Join(words, " ")
		} else {
			line = regexp.MustCompile(`\s+#.*$`).ReplaceAllString(line, "")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
func literalShellSources(text, home, config, zdotdir string) []string {
	paths := []string{}
	pattern := regexp.MustCompile(`(?:^|[; ]|&&|\|\|)(?:source|\.)\s+([^;]+)`)
	unresolved := regexp.MustCompile("[$`*?{}()\\n]")
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			words, err := inspectShellWords(match[1])
			if err != nil || len(words) == 0 {
				continue
			}
			value := words[0]
			for name, path := range map[string]string{"HOME": home, "XDG_CONFIG_HOME": config, "ZDOTDIR": zdotdir} {
				value = strings.ReplaceAll(value, "${"+name+"}", path)
				value = regexp.MustCompile(`\$`+name+`\b`).ReplaceAllStringFunc(value, func(string) string { return path })
			}
			if strings.HasPrefix(value, "~/") {
				value = filepath.Join(home, value[2:])
			}
			if filepath.IsAbs(value) && !unresolved.MatchString(value) {
				paths = append(paths, value)
			}
		}
	}
	return paths
}
func inspectShellSources(shell, rc, base, home, config, zdotdir string) (map[string][]string, []string) {
	type source struct {
		path, content string
		provided      bool
		depth         int
	}
	queue := []source{{rc, base, true, 0}}
	system := map[string]string{"bash": "/etc/bash.bashrc", "zsh": "/etc/zsh/zshrc", "fish": "/etc/fish/config.fish"}
	queue = append(queue, source{path: system[shell]})
	if shell == "fish" {
		masked := map[string]bool{}
		for _, dir := range []string{filepath.Join(config, "fish/conf.d"), "/etc/fish/conf.d", "/usr/share/fish/vendor_conf.d"} {
			paths, _ := filepath.Glob(filepath.Join(dir, "*.fish"))
			sort.Strings(paths)
			for _, path := range paths {
				name := filepath.Base(path)
				if !masked[name] {
					masked[name] = true
					queue = append(queue, source{path: path})
				}
			}
		}
	}
	recognized := map[string][]string{}
	for _, tool := range shellTools {
		recognized[tool] = []string{}
	}
	conflicts := []string{}
	seen := map[string]bool{}
	patterns := map[string]string{
		"mise":     `(?:^|[\s(;|&])(?:[^\s;$\x60()]*/)?mise\s+activate\b`,
		"zoxide":   `(?:^|[\s(;|&])(?:[^\s;$\x60()]*/)?zoxide\s+init\b`,
		"starship": `(?:^|[\s(;|&])(?:[^\s;$\x60()]*/)?starship\s+init\b`,
		"direnv":   `(?:^|[\s(;|&])(?:[^\s;$\x60()]*/)?direnv\s+hook\b`,
		"fzf":      `\bfzf\s+--(?:bash|zsh|fish)\b|(?:source|\.)\s+[^\n]*(?:fzf\.(?:bash|zsh)|fzf/(?:shell/)?(?:key-bindings|completion))|\bfzf_key_bindings\b`,
	}
	promptPatterns := []struct{ label, pattern string }{
		{"Powerlevel10k/Powerlevel9k", `powerlevel(?:9|10)k|\.p10k\.zsh|\bp10k\s+`},
		{"Oh My Posh", `oh-my-posh\s+init`},
		{"Oh My Zsh/Prezto framework", `oh-my-zsh\.sh|prezto|\bZSH_THEME\s*=`},
		{"custom Fish prompt", `\bfunction\s+fish_(?:right_)?prompt\b`},
		{"Tide/Fish prompt theme", `tide configure|\b_tide_|bobthefish|pure\.fish`},
	}
	for count := 0; len(queue) > 0 && count < 96; count++ {
		item := queue[0]
		queue = queue[1:]
		key, err := filepath.EvalSymlinks(item.path)
		if err != nil {
			key = item.path
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		content := item.content
		if !item.provided {
			info, err := os.Stat(item.path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 512*1024 {
				continue
			}
			content, err = shellText(item.path)
			if err != nil {
				continue
			}
			content, err = withoutShellBlock(content, item.path)
			if err != nil {
				continue
			}
		}
		active := activeShellText(content)
		plugins := regexp.MustCompile(`(?s)\bplugins\s*=\s*\((.*?)\)`).FindAllStringSubmatch(active, -1)
		for _, tool := range shellTools {
			found := regexp.MustCompile(patterns[tool]).MatchString(active)
			for _, p := range plugins {
				for _, word := range strings.Fields(p[1]) {
					if word == tool {
						found = true
					}
				}
			}
			if found {
				recognized[tool] = append(recognized[tool], item.path)
			}
		}
		for _, p := range promptPatterns {
			if regexp.MustCompile(`(?i)` + p.pattern).MatchString(active) {
				conflicts = append(conflicts, p.label+" in "+item.path)
			}
		}
		if item.depth < 3 {
			for _, path := range literalShellSources(content, home, config, zdotdir) {
				queue = append(queue, source{path: path, depth: item.depth + 1})
			}
		}
	}
	return recognized, conflicts
}
func posixShellBody(shell string, recognized map[string][]string) string {
	var b strings.Builder
	b.WriteString("# Editor and terminal defaults; open a new shell after setup.\n")
	for _, name := range editorVars {
		fmt.Fprintf(&b, "export %s='code --wait'\n", name)
	}
	b.WriteString(`export TERMINAL='ghostty'
export KIND_EXPERIMENTAL_PROVIDER='podman'
case ":${PATH-}:" in
    *":$HOME/.local/bin:"*) ;;
    *) export PATH="${PATH:+$PATH:}$HOME/.local/bin" ;;
esac
if [[ $- == *i* ]]; then
`)
	init := func(tool, command string) {
		if len(recognized[tool]) > 0 {
			fmt.Fprintf(&b, "    # Existing %s integration retained.\n", tool)
			return
		}
		fmt.Fprintf(&b, "    if command -v %s >/dev/null 2>&1 && [[ \"${__cachyos_tooling_%s_initialized-}\" != 1 ]]; then\n", tool, tool)
		fmt.Fprintf(&b, "        if _cachyos_tooling_init=\"$(%s 2>/dev/null)\"; then\n", command)
		fmt.Fprintf(&b, "            eval \"$_cachyos_tooling_init\"\n            __cachyos_tooling_%s_initialized=1\n        fi\n    fi\n", tool)
	}
	init("mise", "mise activate "+shell)
	b.WriteString(`    _cachyos_tooling_go_bin=""
    if command -v go >/dev/null 2>&1; then
        _cachyos_tooling_go_bin="$(GOTOOLCHAIN=local go env GOBIN 2>/dev/null)" || _cachyos_tooling_go_bin=""
        if [[ -z "$_cachyos_tooling_go_bin" ]]; then
            _cachyos_tooling_go_bin="$(GOTOOLCHAIN=local go env GOPATH 2>/dev/null)" || _cachyos_tooling_go_bin=""
            _cachyos_tooling_go_bin="${_cachyos_tooling_go_bin%%:*}"
            if [[ -n "$_cachyos_tooling_go_bin" ]]; then
                _cachyos_tooling_go_bin="${_cachyos_tooling_go_bin%/}/bin"
            fi
        fi
    fi
    if [[ -n "$_cachyos_tooling_go_bin" ]]; then
        case ":${PATH-}:" in
            *":$_cachyos_tooling_go_bin:"*) ;;
            *) export PATH="${PATH:+$PATH:}$_cachyos_tooling_go_bin" ;;
        esac
    fi
`)
	if len(recognized["zoxide"]) > 0 {
		b.WriteString("    # Existing zoxide integration retained.\n")
	} else {
		b.WriteString("    if command -v zoxide >/dev/null 2>&1 && [[ \"${__cachyos_tooling_zoxide_initialized-}\" != 1 ]]; then\n        if type z >/dev/null 2>&1 || type zi >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "            _cachyos_tooling_init=\"$(zoxide init %s --no-cmd 2>/dev/null)\" || _cachyos_tooling_init=\"\"\n        else\n            _cachyos_tooling_init=\"$(zoxide init %s 2>/dev/null)\" || _cachyos_tooling_init=\"\"\n", shell, shell)
		b.WriteString("        fi\n        if [[ -n \"$_cachyos_tooling_init\" ]]; then\n            eval \"$_cachyos_tooling_init\"\n            __cachyos_tooling_zoxide_initialized=1\n        fi\n    fi\n")
	}
	init("fzf", "fzf --"+shell)
	init("starship", "starship init "+shell)
	b.WriteString("    # direnv stays last among integrations managed here.\n")
	init("direnv", "direnv hook "+shell)
	b.WriteString("    unset _cachyos_tooling_init _cachyos_tooling_go_bin\nfi\n")
	return b.String()
}
func fishShellBody(recognized map[string][]string) string {
	var b strings.Builder
	b.WriteString("# Editor and terminal defaults; open a new shell after setup.\n")
	for _, name := range editorVars {
		fmt.Fprintf(&b, "set -gx %s 'code --wait'\n", name)
	}
	b.WriteString(`set -gx TERMINAL ghostty
set -gx KIND_EXPERIMENTAL_PROVIDER podman
if not contains -- "$HOME/.local/bin" $PATH
    set -gx PATH $PATH "$HOME/.local/bin"
end
if status is-interactive
`)
	init := func(tool, command string) {
		if len(recognized[tool]) > 0 {
			fmt.Fprintf(&b, "    # Existing %s integration retained.\n", tool)
			return
		}
		fmt.Fprintf(&b, "    if command -q %s; and not set -q __cachyos_tooling_%s_initialized\n        set -l _cachyos_tooling_init (%s 2>/dev/null)\n", tool, tool, command)
		fmt.Fprintf(&b, "        if test $status -eq 0\n            printf '%%s\\n' $_cachyos_tooling_init | source\n            set -g __cachyos_tooling_%s_initialized 1\n        end\n    end\n", tool)
	}
	init("mise", "mise activate fish")
	b.WriteString(`    set -l _cachyos_tooling_go_bin ''
    if command -q go
        set _cachyos_tooling_go_bin (env GOTOOLCHAIN=local go env GOBIN 2>/dev/null)
        if not test -n "$_cachyos_tooling_go_bin"
            set -l _cachyos_tooling_go_path (env GOTOOLCHAIN=local go env GOPATH 2>/dev/null)
            set _cachyos_tooling_go_path (string split -m 1 : -- "$_cachyos_tooling_go_path")[1]
            if test -n "$_cachyos_tooling_go_path"
                set _cachyos_tooling_go_bin (string trim -r -c / -- "$_cachyos_tooling_go_path")/bin
            end
        end
    end
    if test -n "$_cachyos_tooling_go_bin"; and not contains -- "$_cachyos_tooling_go_bin" $PATH
        set -gx PATH $PATH "$_cachyos_tooling_go_bin"
    end
`)
	if len(recognized["zoxide"]) > 0 {
		b.WriteString("    # Existing zoxide integration retained.\n")
	} else {
		b.WriteString(`    if command -q zoxide; and not set -q __cachyos_tooling_zoxide_initialized
        set -l _cachyos_tooling_init
        if type -q z; or type -q zi
            set _cachyos_tooling_init (zoxide init fish --no-cmd 2>/dev/null)
        else
            set _cachyos_tooling_init (zoxide init fish 2>/dev/null)
        end
        if test -n "$_cachyos_tooling_init"
            printf '%s\n' $_cachyos_tooling_init | source
            set -g __cachyos_tooling_zoxide_initialized 1
        end
    end
`)
	}
	init("fzf", "fzf --fish")
	init("starship", "starship init fish")
	b.WriteString("    # direnv stays last among integrations managed here.\n")
	init("direnv", "direnv hook fish")
	b.WriteString("end\n")
	return b.String()
}
func validateShell(c *Context, shell, text string, required bool) (string, error) {
	if !hasCommand(shell) {
		if required {
			return "", fmt.Errorf("%s parser required for environment validation", shell)
		}
		return "not installed; parser unavailable", nil
	}
	flags := map[string][]string{"bash": {"--noprofile", "--norc", "-n"}, "zsh": {"-f", "-n"}, "fish": {"--no-config", "-n"}, "sh": {"-n"}}
	args := append([]string{shell}, flags[shell]...)
	_, err := c.Command(Command{Args: args, Input: []byte(text), Env: map[string]string{"BASH_ENV": "", "ENV": ""}, Timeout: 20 * time.Second})
	if err != nil {
		return "", fmt.Errorf("%s syntax validation failed; configuration unchanged: %w", shell, err)
	}
	return "passed", nil
}
func ConfigureShell(c *Context) error {
	zdotdir := os.Getenv("ZDOTDIR")
	if zdotdir == "" {
		zdotdir = c.Home
	}
	if strings.HasPrefix(zdotdir, "~/") {
		zdotdir = filepath.Join(c.Home, zdotdir[2:])
	}
	zdotdir, _ = filepath.Abs(zdotdir)
	paths := map[string]string{"bash": filepath.Join(c.Home, ".bashrc"), "zsh": filepath.Join(zdotdir, ".zshrc"), "fish": filepath.Join(c.ConfigHome, "fish/config.fish")}
	type writePlan struct{ path, text string }
	plans := []writePlan{}
	shellReport := map[string]any{}
	checks := map[string]any{}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		path := paths[shell]
		_, pathErr := os.Lstat(path)
		if shell != "bash" && !hasCommand(shell) && os.IsNotExist(pathErr) {
			continue
		}
		text, err := shellText(path)
		if err != nil {
			return err
		}
		base, err := withoutShellBlock(text, path)
		if err != nil {
			return err
		}
		recognized, conflicts := inspectShellSources(shell, path, base, c.Home, c.ConfigHome, zdotdir)
		bodyTools := map[string][]string{}
		for key, value := range recognized {
			bodyTools[key] = append([]string{}, value...)
		}
		if len(conflicts) > 0 {
			bodyTools["starship"] = append(bodyTools["starship"], "existing prompt framework")
			c.Warn(shell + ": preserving existing prompt: " + strings.Join(conflicts, ", "))
		}
		if len(recognized["direnv"]) > 0 {
			c.Note(shell + ": existing direnv hook retained; check its order if directory activation misbehaves")
		}
		body := posixShellBody(shell, bodyTools)
		if shell == "fish" {
			body = fishShellBody(bodyTools)
		}
		checks[shell], err = validateShell(c, shell, body, false)
		if err != nil {
			return err
		}
		plans = append(plans, writePlan{path, withShellBlock(base, body)})
		shellReport[shell] = map[string]any{"path": path, "existing_initializations": recognized, "preserved_prompts": conflicts}
	}
	for _, session := range []string{"environment.d/90-cachyos-tooling.conf", "uwsm/env"} {
		path := filepath.Join(c.ConfigHome, session)
		text, err := shellText(path)
		if err != nil {
			return err
		}
		base, err := withoutShellBlock(text, path)
		if err != nil {
			return err
		}
		var body strings.Builder
		body.WriteString("# Applies at the next login.\n")
		for _, name := range editorVars {
			if session == "uwsm/env" {
				fmt.Fprintf(&body, "export %s='code --wait'\n", name)
			} else {
				fmt.Fprintf(&body, "%s=\"code --wait\"\n", name)
			}
		}
		if session == "uwsm/env" {
			body.WriteString("export TERMINAL='ghostty'\nexport KIND_EXPERIMENTAL_PROVIDER='podman'\n")
		} else {
			body.WriteString("TERMINAL=ghostty\nKIND_EXPERIMENTAL_PROVIDER=podman\n")
		}
		complete := withShellBlock(base, body.String())
		if session == "uwsm/env" {
			checks[session], err = validateShell(c, "sh", complete, true)
			if err != nil {
				return err
			}
		}
		plans = append(plans, writePlan{path, complete})
	}
	// Complete all inspection and parser checks before the first journalled write.
	for _, plan := range plans {
		if err := c.Write(plan.path, []byte(plan.text), 0600); err != nil {
			return err
		}
	}
	c.Report["shells"] = shellReport
	c.Report["shell_syntax_checks"] = checks
	c.Note("Shell defaults configured; open a new terminal, and log in again for GUI defaults.")
	return c.Save()
}

// TOML edits are deliberately narrow: standard tables/values, no multiline
// strings or dotted compose-provider declarations. Unknown layouts stay intact.
type tomlStatement struct {
	start, end int
	text       string
}
type composeSelection struct {
	tableEnd, start, end int
	value                string
	found, table         bool
}

func tomlStatements(text string) ([]tomlStatement, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("configuration is not valid UTF-8")
	}
	if strings.Contains(text, `"""`) || strings.Contains(text, "'''") {
		return nil, fmt.Errorf("multiline TOML strings require manual provider selection; file unchanged")
	}
	statements := []tomlStatement{}
	start := 0
	quote := byte(0)
	escaped, comment := false, false
	stack := []byte{}
	emit := func(end int) {
		part := text[start:end]
		clean := strings.TrimSpace(tomlWithoutComments(part))
		if clean != "" {
			statements = append(statements, tomlStatement{start, end, clean})
		}
		start = end
	}
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if comment {
			if ch == '\n' {
				comment = false
				if len(stack) == 0 {
					emit(i + 1)
				}
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			if ch == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			if ch == '\n' {
				return nil, fmt.Errorf("newline in TOML string")
			}
			continue
		}
		if ch == '#' {
			comment = true
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '[' || ch == '{' {
			stack = append(stack, ch)
		} else if ch == ']' || ch == '}' {
			if len(stack) == 0 || stack[len(stack)-1] != map[byte]byte{']': '[', '}': '{'}[ch] {
				return nil, fmt.Errorf("unbalanced TOML delimiters")
			}
			stack = stack[:len(stack)-1]
		}
		if ch == '\n' && len(stack) == 0 {
			emit(i + 1)
		}
	}
	if quote != 0 || escaped || len(stack) > 0 {
		return nil, fmt.Errorf("incomplete TOML value")
	}
	if start < len(text) {
		emit(len(text))
	}
	return statements, nil
}
func tomlWithoutComments(text string) string {
	var b strings.Builder
	quote := byte(0)
	escaped, comment := false, false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if comment {
			if ch == '\n' {
				comment = false
				b.WriteByte(ch)
			}
			continue
		}
		if escaped {
			escaped = false
			b.WriteByte(ch)
			continue
		}
		if quote != 0 {
			b.WriteByte(ch)
			if ch == '\\' && quote == '"' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '#' {
			comment = true
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
		}
		b.WriteByte(ch)
	}
	return b.String()
}
func tomlDelimiter(text string, delimiter byte) int {
	quote := byte(0)
	escaped := false
	depth := 0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			if ch == '\\' && quote == '"' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == delimiter && depth == 0 {
			return i
		}
		if ch == '[' || ch == '{' {
			depth++
		} else if ch == ']' || ch == '}' {
			depth--
		}
	}
	return -1
}
func tomlParts(text string, delimiter byte) []string {
	parts := []string{}
	for {
		index := tomlDelimiter(text, delimiter)
		if index < 0 {
			return append(parts, text)
		}
		parts = append(parts, text[:index])
		text = text[index+1:]
	}
}
func tomlKey(text string) (string, error) {
	parts := tomlParts(strings.TrimSpace(text), '.')
	normalized := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", fmt.Errorf("empty TOML key")
		}
		if part[0] == '"' || part[0] == '\'' {
			value, err := tomlString(part)
			if err != nil {
				return "", err
			}
			normalized = append(normalized, value)
		} else {
			if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(part) {
				return "", fmt.Errorf("unsupported TOML key: %s", part)
			}
			normalized = append(normalized, part)
		}
	}
	return strings.Join(normalized, "."), nil
}
func tomlString(text string) (string, error) {
	for _, ch := range text {
		if ch < 32 && ch != 9 || ch == 127 {
			return "", fmt.Errorf("control character in TOML string")
		}
	}
	if len(text) < 2 || text[0] != text[len(text)-1] {
		return "", fmt.Errorf("invalid TOML string")
	}
	if text[0] == '\'' {
		if strings.ContainsAny(text[1:len(text)-1], "'\n\r") {
			return "", fmt.Errorf("invalid TOML literal string")
		}
		return text[1 : len(text)-1], nil
	}
	// Go and TOML share these escapes; reject the extra Go-only escapes.
	for i := 1; i < len(text)-1; i++ {
		if text[i] == '\\' {
			i++
			if i >= len(text)-1 || !strings.ContainsRune(`btnfr"\uU`, rune(text[i])) {
				return "", fmt.Errorf("unsupported TOML string escape")
			}
		}
	}
	value, err := strconv.Unquote(text)
	if err != nil {
		return "", fmt.Errorf("invalid TOML string: %w", err)
	}
	return value, nil
}
func validTOMLValue(value string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("TOML nesting exceeds safe inspection limit")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("missing TOML value")
	}
	switch value[0] {
	case '"', '\'':
		_, err := tomlString(value)
		return err
	case '[':
		if !strings.HasSuffix(value, "]") {
			return fmt.Errorf("incomplete TOML array")
		}
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return nil
		}
		parts := tomlParts(inner, ',')
		for i, part := range parts {
			if strings.TrimSpace(part) == "" && i == len(parts)-1 {
				continue
			}
			if err := validTOMLValue(part, depth+1); err != nil {
				return err
			}
		}
		return nil
	case '{':
		if !strings.HasSuffix(value, "}") {
			return fmt.Errorf("incomplete TOML inline table")
		}
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return nil
		}
		seen := map[string]bool{}
		for _, part := range tomlParts(inner, ',') {
			index := tomlDelimiter(part, '=')
			if index < 0 {
				return fmt.Errorf("invalid TOML inline table")
			}
			key, err := tomlKey(part[:index])
			if err != nil {
				return err
			}
			if seen[key] {
				return fmt.Errorf("duplicate TOML inline key")
			}
			seen[key] = true
			if err := validTOMLValue(part[index+1:], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if value == "true" || value == "false" {
		return nil
	}
	number := `^[+-]?(?:inf|nan|(?:0|[1-9][0-9]*(?:_[0-9]+)*)(?:\.[0-9]+(?:_[0-9]+)*)?(?:[eE][+-]?[0-9]+(?:_[0-9]+)*)?)$|^0x[0-9A-Fa-f]+(?:_[0-9A-Fa-f]+)*$|^0o[0-7]+(?:_[0-7]+)*$|^0b[01]+(?:_[01]+)*$`
	date := `^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}(?:[Tt ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:[Zz]|[+-][0-9]{2}:[0-9]{2})?)?|[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?)$`
	if regexp.MustCompile(number).MatchString(value) {
		return nil
	}
	if regexp.MustCompile(date).MatchString(value) {
		normalized := strings.ReplaceAll(strings.ReplaceAll(value, "t", "T"), "z", "Z")
		if len(normalized) > 10 && normalized[10] == ' ' {
			normalized = normalized[:10] + "T" + normalized[11:]
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02", "15:04:05.999999999"} {
			if _, err := time.Parse(layout, normalized); err == nil {
				return nil
			}
		}
		return fmt.Errorf("invalid TOML date or time: %s", value)
	}
	return fmt.Errorf("unsupported or invalid TOML value: %s", value)
}
func scanComposeConfig(text string) (composeSelection, error) {
	result := composeSelection{start: -1, end: -1}
	statements, err := tomlStatements(text)
	if err != nil {
		return result, err
	}
	table := ""
	scope := ""
	arrayIndex := 0
	tables := map[string]bool{}
	keys := map[string]bool{}
	for _, statement := range statements {
		value := statement.text
		if strings.HasPrefix(value, "[") {
			array := strings.HasPrefix(value, "[[")
			closing := "]"
			open := 1
			if array {
				closing = "]]"
				open = 2
			}
			if !strings.HasSuffix(value, closing) {
				return result, fmt.Errorf("invalid TOML table")
			}
			name, err := tomlKey(value[open : len(value)-len(closing)])
			if err != nil {
				return result, err
			}
			if !array && tables[name] {
				return result, fmt.Errorf("duplicate TOML table %s", name)
			}
			if array && (name == "engine" || strings.HasPrefix(name, "engine.")) {
				return result, fmt.Errorf("engine is an array table; file unchanged")
			}
			tables[name] = true
			table = name
			scope = name
			if array {
				arrayIndex++
				scope += "#" + strconv.Itoa(arrayIndex)
			}
			if name == "engine" {
				if value != "[engine]" {
					return result, fmt.Errorf("use ordinary [engine] table for safe provider editing")
				}
				result.table = true
				result.tableEnd = statement.end
			}
			continue
		}
		index := tomlDelimiter(value, '=')
		if index < 0 {
			return result, fmt.Errorf("invalid TOML assignment")
		}
		key, err := tomlKey(value[:index])
		if err != nil {
			return result, err
		}
		qualified := key
		if table != "" {
			qualified = table + "." + key
		}
		scopeKey := scope + "." + key
		if keys[scopeKey] {
			return result, fmt.Errorf("duplicate TOML key %s", qualified)
		}
		keys[scopeKey] = true
		setting := strings.TrimSpace(value[index+1:])
		if err := validTOMLValue(setting, 0); err != nil {
			return result, err
		}
		if qualified == "engine" {
			return result, fmt.Errorf("engine is not an ordinary TOML table")
		}
		if qualified == "engine.compose_providers" {
			if table != "engine" || key != "compose_providers" {
				return result, fmt.Errorf("use ordinary engine.compose_providers assignment; file unchanged")
			}
			if !strings.HasPrefix(setting, "[") {
				return result, fmt.Errorf("compose_providers must be an array")
			}
			result.found = true
			result.start = statement.start
			result.end = statement.end
			result.value = setting
		}
	}
	return result, nil
}
func composeConfigured(selection composeSelection, allowSingle bool) bool {
	if !selection.found {
		return false
	}
	parts := tomlParts(strings.TrimSpace(selection.value)[1:len(strings.TrimSpace(selection.value))-1], ',')
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	provider, err := tomlString(strings.TrimSpace(parts[0]))
	if err != nil || provider != composeProvider {
		return false
	}
	if len(parts) == 1 {
		return allowSingle
	}
	appendSetting := regexp.MustCompile(`\s+`).ReplaceAllString(parts[1], "")
	return appendSetting == "{append=false}" || appendSetting == `{"append"=false}` || appendSetting == "{'append'=false}"
}
func selectComposeProvider(text string) (string, error) {
	selection, err := scanComposeConfig(text)
	if err != nil {
		return "", err
	}
	if composeConfigured(selection, false) {
		return text, nil
	}
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	setting := `compose_providers = ["` + composeProvider + `", {append=false}]`
	result := ""
	if selection.found {
		original := text[selection.start:selection.end]
		indent := regexp.MustCompile(`^[ \t]*`).FindString(original)
		comment := ""
		last := strings.TrimRight(original, "\r\n")
		if index := tomlDelimiter(last, '#'); index >= 0 {
			comment = " " + last[index:]
		}
		ending := ""
		if strings.HasSuffix(original, "\n") {
			ending = newline
		}
		result = text[:selection.start] + indent + setting + comment + ending + text[selection.end:]
	} else if selection.table {
		separator := ""
		if selection.tableEnd > 0 && text[selection.tableEnd-1] != '\n' {
			separator = newline
		}
		result = text[:selection.tableEnd] + separator + setting + newline + text[selection.tableEnd:]
	} else {
		separator := ""
		if text != "" && !strings.HasSuffix(text, newline+newline) {
			separator = newline
			if !strings.HasSuffix(text, "\n") {
				separator += newline
			}
		}
		result = text + separator + "[engine]" + newline + setting + newline
	}
	after, err := scanComposeConfig(result)
	if err != nil || !composeConfigured(after, false) {
		return "", fmt.Errorf("proposed provider edit failed static TOML validation; file unchanged")
	}
	return result, nil
}

type subordinateRange struct {
	Start uint64 `json:"start"`
	Count uint64 `json:"count"`
}

func parseSubordinateRanges(output string) ([]subordinateRange, bool, error) {
	ranges := []subordinateRange{}
	pattern := regexp.MustCompile(`^\s*\d+:\s+\S+\s+(\d+)\s+(\d+)\s*$`)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			return nil, false, fmt.Errorf("unrecognized getsubids output")
		}
		start, err1 := strconv.ParseUint(match[1], 10, 64)
		count, err2 := strconv.ParseUint(match[2], 10, 64)
		if err1 != nil || err2 != nil || start == 0 || count == 0 || start > 4294967295 || count > 4294967295-start {
			return nil, false, fmt.Errorf("invalid subordinate-ID range")
		}
		ranges = append(ranges, subordinateRange{start, count})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	ready := false
	for i, r := range ranges {
		if i > 0 && ranges[i-1].Start+ranges[i-1].Count > r.Start {
			return ranges, false, fmt.Errorf("overlapping subordinate-ID ranges")
		}
		if r.Count >= 65536 {
			ready = true
		}
	}
	return ranges, ready, nil
}
func subordinateIDs(c *Context, username string, groups bool) map[string]any {
	args := []string{"getsubids"}
	if groups {
		args = append(args, "-g")
	}
	args = append(args, username)
	result, err := c.Command(Command{Args: args, Timeout: 10 * time.Second, Env: map[string]string{"LC_ALL": "C"}})
	report := map[string]any{"ranges": []subordinateRange{}, "ready": false, "minimum_contiguous_count": 65536, "error": nil}
	if err != nil {
		report["error"] = err.Error()
		return report
	}
	ranges, ready, err := parseSubordinateRanges(result.Stdout)
	report["ranges"] = ranges
	report["ready"] = ready
	if err != nil {
		report["error"] = err.Error()
	}
	return report
}
func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && syscall.Access(path, 1) == nil
}
func containerReadiness(c *Context) map[string]any {
	username := os.Getenv("USER")
	if current, err := user.Current(); err == nil {
		username = current.Username
	}
	uids, gids := subordinateIDs(c, username, false), subordinateIDs(c, username, true)
	helpers := map[string]bool{"newuidmap": hasCommand("newuidmap"), "newgidmap": hasCommand("newgidmap")}
	cgroup := exists("/sys/fs/cgroup/cgroup.controllers")
	ready := uids["ready"] == true && gids["ready"] == true && helpers["newuidmap"] && helpers["newgidmap"] && cgroup
	for label, mapping := range map[string]map[string]any{"UID": uids, "GID": gids} {
		if mapping["ready"] != true {
			c.Warn(fmt.Sprintf("Rootless Podman pending: %s needs a nonoverlapping subordinate %s range of at least 65536 IDs; have an administrator allocate a free range without replacing existing mappings. %v", username, label, mapping["error"]))
		}
	}
	for name, present := range helpers {
		if !present {
			c.Warn("Rootless Podman pending: " + name + " unavailable; install shadow.")
		}
	}
	if !cgroup {
		c.Warn("Rootless Kind pending: cgroup v2 unavailable; review host configuration.")
	}
	namespace := map[string]any{}
	for _, path := range []string{"/proc/sys/user/max_user_namespaces", "/proc/sys/kernel/unprivileged_userns_clone"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			continue
		}
		namespace[path] = value
		if value <= 0 {
			ready = false
			c.Warn("Rootless containers pending: " + path + " disables user namespaces; no kernel settings were changed.")
		}
	}
	return map[string]any{"user": username, "subuids": uids, "subgids": gids, "idmap_helpers": helpers, "cgroup_v2": cgroup, "user_namespace_settings": namespace, "ready": ready, "scope": "Static prerequisites only; no namespaces, Podman storage or containers initialized."}
}
func laterComposeOverrides(path string) ([]string, []string) {
	conflicts, errors := []string{}, []string{}
	candidates, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.conf"))
	sort.Strings(candidates)
	for _, candidate := range candidates {
		if filepath.Base(candidate) <= filepath.Base(path) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
			errors = append(errors, candidate+": not a regular configuration file under 2 MiB")
			continue
		}
		text, err := shellText(candidate)
		if err != nil {
			errors = append(errors, candidate+": "+err.Error())
			continue
		}
		selection, err := scanComposeConfig(text)
		if err != nil {
			errors = append(errors, candidate+": "+err.Error())
			continue
		}
		if selection.found && !composeConfigured(selection, true) {
			conflicts = append(conflicts, candidate)
		}
	}
	return conflicts, errors
}
func inspectContainers(c *Context, configure bool) error {
	path := filepath.Join(c.ConfigHome, "containers/containers.conf.d/90-myarch-buildkit.conf")
	readPath := path
	if configure {
		readPath = c.View(path)
	}
	text, err := shellText(readPath)
	if err != nil {
		return err
	}
	selection, err := scanComposeConfig(text)
	if err != nil {
		return fmt.Errorf("Podman configuration unchanged: %w", err)
	}
	if configure {
		replacement, err := selectComposeProvider(text)
		if err != nil {
			return err
		}
		current, err := shellText(readPath)
		if err != nil {
			return err
		}
		if current != text {
			return fmt.Errorf("Podman configuration changed during inspection; rerun")
		}
		if err := c.Write(path, []byte(replacement), 0600); err != nil {
			return err
		}
		selection, err = scanComposeConfig(replacement)
		if err != nil {
			return err
		}
	}
	configured := composeConfigured(selection, false)
	providerExists := executableFile(composeProvider)
	readiness := containerReadiness(c)
	overrides := []string{}
	for _, key := range []string{"CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE"} {
		if os.Getenv(key) != "" {
			overrides = append(overrides, key)
		}
	}
	if provider := os.Getenv("PODMAN_COMPOSE_PROVIDER"); provider != "" && provider != composeProvider {
		overrides = append(overrides, "PODMAN_COMPOSE_PROVIDER")
	}
	conflicts, inspectionErrors := laterComposeOverrides(path)
	overrides = append(overrides, conflicts...)
	overrides = append(overrides, inspectionErrors...)
	if !configured {
		c.Warn("Podman Compose provider pending; configure the managed drop-in.")
	}
	if !providerExists {
		c.Warn("Podman Compose missing or not executable: " + composeProvider)
	}
	if len(overrides) > 0 {
		c.Warn("Effective Compose provider unverified: " + strings.Join(overrides, ", "))
	}
	ready := configured && providerExists && readiness["ready"] == true && len(overrides) == 0
	c.Report["containers"] = map[string]any{"compose_provider": composeProvider, "compose_provider_configured": configured, "compose_provider_executable": providerExists, "readiness": readiness, "later_provider_overrides": conflicts, "dropin_inspection_errors": inspectionErrors, "configuration_overrides": overrides, "effective_provider_unverified": len(overrides) > 0, "ready": ready, "configuration_scope": "Static user drop-in inspection; system ordering, modules and runtime effective selection not evaluated."}
	if configure && c.Options.EnablePodmanSocket {
		if _, err := c.Run("systemctl", "--user", "enable", "--now", "podman.socket"); err != nil {
			return err
		}
		c.Report["podman_socket"] = map[string]any{"explicitly_enabled": true, "restore_policy": "Explicit socket opt-in is not automatically reversed by file restoration."}
	}
	if configure {
		if err := c.Save(); err != nil {
			return err
		}
	}
	if !configure && !ready {
		return fmt.Errorf("rootless containers or Compose provider pending; see report")
	}
	return nil
}
func ConfigureContainers(c *Context) error { return inspectContainers(c, true) }
func CheckContainers(c *Context) error     { return inspectContainers(c, false) }
