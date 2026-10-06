package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var inputNameCharacters = regexp.MustCompile(`[^a-z0-9_-]`)
var internalOutputName = regexp.MustCompile(`^(?:eDP|LVDS|DSI)-[A-Za-z0-9_.:-]+$`)
var safeInputName = regexp.MustCompile(`^[a-z0-9_-]+$`)
var scaleValue = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?|[0-9]+/[0-9]+)$`)

func ParseLaptopScale(value any) (float64, error) {
	text := strings.TrimSpace(str(value))
	if !scaleValue.MatchString(text) {
		return 0, fmt.Errorf("laptop scale expects a decimal or ratio, for example 1.6 or 4/3")
	}
	parts := strings.Split(text, "/")
	n, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, err
	}
	if len(parts) == 2 {
		d, e := strconv.ParseFloat(parts[1], 64)
		if e != nil || d == 0 {
			return 0, fmt.Errorf("laptop scale must be a finite positive decimal or ratio")
		}
		n /= d
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < .25 || n > 8 {
		return 0, fmt.Errorf("laptop scale must be between 0.25 and 8")
	}
	return n, nil
}

// CompatibleScale follows the compositor's float storage and first n/120
// normalization. It never substitutes a different requested scale.
func CompatibleScale(width, height, scale float64) ([]int, error) {
	if width <= 0 || height <= 0 || scale <= 0 || math.IsInf(scale, 0) || math.IsNaN(scale) || math.Trunc(width) != width || math.Trunc(height) != height {
		return nil, fmt.Errorf("invalid display dimensions or scale")
	}
	stored := float64(float32(scale))
	if stored <= 0 || math.IsInf(stored, 0) {
		return nil, fmt.Errorf("invalid stored display scale")
	}
	logical := func(s float64) ([]int, bool) {
		w, h := width/s, height/s
		if w == math.Round(w) && h == math.Round(h) {
			return []int{int(w), int(h)}, true
		}
		return nil, false
	}
	if dimensions, ok := logical(stored); ok {
		return dimensions, nil
	}
	step := math.Floor(stored*120 + .5)
	normalized := step / 120
	if step > 0 && float32(normalized) == float32(stored) {
		if dimensions, ok := logical(normalized); ok {
			return dimensions, nil
		}
	}
	return nil, fmt.Errorf("scale %g is incompatible with %gx%g; no alternative was selected", scale, width, height)
}

func objects(value any) []map[string]any {
	result := []map[string]any{}
	switch values := value.(type) {
	case []any:
		for _, v := range values {
			if m, ok := v.(map[string]any); ok {
				result = append(result, m)
			}
		}
	case []map[string]any:
		return values
	}
	return result
}

// KeyboardRoles accepts only exact normalized kernel names verified by udev.
// USB and Bluetooth keyboards never inherit the built-in keyboard layout.
func KeyboardRoles(c *Context) (map[string][]string, error) {
	actual := map[string]bool{}
	for _, keyboard := range objects(object(c.Inventory["devices"])["keyboards"]) {
		if name, ok := keyboard["name"].(string); ok {
			actual[name] = true
		}
	}
	roles := map[string][]string{"internal": {}, "external": {}, "unmatched": {}}
	for name := range actual {
		roles["unmatched"] = append(roles["unmatched"], name)
	}
	sort.Strings(roles["unmatched"])
	root := "/sys/class/input"
	if configured, ok := c.Inventory["input_root"].(string); ok {
		root = configured
	}
	entries, err := filepath.Glob(filepath.Join(root, "event*"))
	if err != nil {
		return roles, err
	}
	for _, entry := range entries {
		raw, e := os.ReadFile(filepath.Join(entry, "device/name"))
		if e != nil {
			continue
		}
		bus, e := os.ReadFile(filepath.Join(entry, "device/id/bustype"))
		if e != nil {
			continue
		}
		name := inputNameCharacters.ReplaceAllString(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(string(raw))), " ", "-"), "-")
		if !actual[name] {
			continue
		}
		result, queryError := c.Runner.Execute(Command{Args: []string{"udevadm", "info", "--query=property", "--path", entry}, Timeout: 3 * time.Second})
		if queryError != nil || result.Code != 0 {
			continue
		}
		properties := map[string]string{}
		for _, line := range strings.Split(result.Stdout, "\n") {
			if pair := strings.SplitN(line, "=", 2); len(pair) == 2 {
				properties[pair[0]] = pair[1]
			}
		}
		b := strings.ToLower(strings.TrimSpace(string(bus)))
		if properties["ID_INPUT_KEYBOARD"] != "1" || (b != "0011" && b != "0018") || properties["ID_BUS"] == "usb" || properties["ID_BUS"] == "bluetooth" {
			continue
		}
		if !desktopContainsString(roles["internal"], name) {
			roles["internal"] = append(roles["internal"], name)
		}
	}
	unmatched := []string{}
	for _, name := range roles["unmatched"] {
		if !desktopContainsString(roles["internal"], name) {
			unmatched = append(unmatched, name)
		}
	}
	roles["unmatched"] = unmatched
	return roles, nil
}
func desktopContainsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func PreflightLaptop(c *Context) error {
	requested := c.Settings.Laptop.InternalScale
	if requested == nil {
		requested = "4/3"
	}
	if c.Options.LaptopScale != "" {
		requested = c.Options.LaptopScale
	} else if override := os.Getenv("CACHYOS_LAPTOP_SCALE"); override != "" {
		requested = override
	}
	scale, err := ParseLaptopScale(requested)
	if err != nil {
		return err
	}
	outputs := []string{}
	verified := []map[string]any{}
	for _, monitor := range objects(c.Inventory["monitors"]) {
		name, _ := monitor["name"].(string)
		if !internalOutputName.MatchString(name) {
			continue
		}
		outputs = append(outputs, name)
		width, height := mapJSONNumber(monitor["width"]), mapJSONNumber(monitor["height"])
		if width != 0 && height != 0 {
			dimensions, e := CompatibleScale(width, height, scale)
			if e != nil {
				return e
			}
			verified = append(verified, map[string]any{"output": name, "logical_dimensions": dimensions})
		}
	}
	if len(outputs) == 0 {
		return fmt.Errorf("no built-in display was identified; existing display settings were preserved")
	}
	roles, err := KeyboardRoles(c)
	if err != nil {
		return err
	}
	if len(roles["internal"]) == 0 {
		return fmt.Errorf("no built-in keyboard was verified by sysfs/udev; existing keyboard layouts were preserved")
	}
	c.Settings.Laptop.InternalScale = scale
	c.Report["laptop_scale_selection"] = map[string]any{"scale": scale, "internal_outputs": outputs, "internal_keyboards": roles["internal"], "verified_modes": verified}
	return nil
}

func LaptopFragment(c *Context) string {
	suffix := ".conf"
	if filepath.Ext(str(c.Inventory["active_config"])) == ".lua" {
		suffix = ".lua"
	}
	return filepath.Join(c.ConfigHome, "myarch-buildkit/laptop"+suffix)
}
func luaString(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 32:
			fmt.Fprintf(&b, "\\%03d", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
func LaptopConfigText(format string, selection map[string]any, layout string, natural bool) (string, error) {
	value := strconv.FormatBool(natural)
	scale := strconv.FormatFloat(mapJSONNumber(selection["scale"]), 'g', 17, 64)
	lines := []string{}
	if format == "lua" {
		lines = append(lines, "-- Managed scrolling, built-in keyboard and display scale only.", "hl.config({input={natural_scroll="+value+",touchpad={natural_scroll="+value+"}}})")
		for _, keyboard := range stringList(selection["internal_keyboards"]) {
			lines = append(lines, "hl.device({name="+luaString(keyboard)+",kb_layout="+luaString(layout)+"})")
		}
		for _, output := range stringList(selection["internal_outputs"]) {
			lines = append(lines, "hl.monitor({output="+luaString(output)+",mode=\"preferred\",position=\"auto\",scale="+scale+"})")
		}
	} else {
		lines = append(lines, "# Managed scrolling, built-in keyboard and display scale only.", "input {", "  natural_scroll = "+value, "  touchpad {", "    natural_scroll = "+value, "  }", "}")
		for _, keyboard := range stringList(selection["internal_keyboards"]) {
			if !safeInputName.MatchString(keyboard) || !regexp.MustCompile(`^[a-zA-Z0-9_,+-]+$`).MatchString(layout) {
				return "", fmt.Errorf("unsupported internal keyboard name or layout")
			}
			lines = append(lines, "device {", "  name = "+keyboard, "  kb_layout = "+layout, "}")
		}
		for _, output := range stringList(selection["internal_outputs"]) {
			if !internalOutputName.MatchString(output) {
				return "", fmt.Errorf("unsupported built-in display name")
			}
			lines = append(lines, "monitor = "+output+", preferred, auto, "+scale)
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}
func ConfigureLaptop(c *Context) error {
	if err := PreflightLaptop(c); err != nil {
		return err
	}
	selection := object(c.Report["laptop_scale_selection"])
	fragment := LaptopFragment(c)
	format := "hyprlang"
	if filepath.Ext(fragment) == ".lua" {
		format = "lua"
	}
	layout := c.Settings.Laptop.InternalKeyboard
	if layout == "" {
		layout = "hu"
	}
	text, err := LaptopConfigText(format, selection, layout, c.Settings.Laptop.NaturalScroll)
	if err != nil {
		return err
	}
	if err = c.Write(fragment, []byte(text), 0600); err != nil {
		return err
	}
	policy := desktopCloneObject(selection)
	policy["schema_version"] = 2
	policy["internal_scale"] = selection["scale"]
	policy["internal_keyboard"] = layout
	policy["natural_scroll"] = c.Settings.Laptop.NaturalScroll
	policy["fragment"] = fragment
	policy["format"] = format
	if err = desktopWriteObject(c, filepath.Join(c.ConfigHome, "myarch-buildkit/laptop.json"), policy, 0600); err != nil {
		return err
	}
	c.Report["laptop"] = map[string]any{"configured": true, "activated": false, "fragment": fragment, "internal_keyboards": selection["internal_keyboards"], "internal_outputs": selection["internal_outputs"], "requested": map[string]any{"internal_scale": selection["scale"], "internal_keyboard": layout, "natural_scroll": c.Settings.Laptop.NaturalScroll}}
	c.Note("Only natural scrolling, the verified built-in keyboard layout and built-in display scale are staged.")
	return nil
}
func CheckLaptop(c *Context) error {
	report := map[string]any{"scope": "read-only", "ready": false}
	pending := []string{}
	path := filepath.Join(c.ConfigHome, "myarch-buildkit/laptop.json")
	policy, err := desktopReadObject(path, nil)
	if err != nil {
		return err
	}
	if !exists(path) {
		pending = append(pending, "The installed laptop settings are missing")
	} else {
		monitors, e := c.Run("hyprctl", "-j", "monitors", "all")
		if e != nil {
			return e
		}
		var values []map[string]any
		if e = json.Unmarshal([]byte(monitors.Stdout), &values); e != nil {
			return e
		}
		actual := map[string]map[string]any{}
		for _, m := range values {
			actual[str(m["name"])] = m
		}
		for _, name := range stringList(policy["internal_outputs"]) {
			if math.Abs(mapJSONNumber(actual[name]["scale"])-mapJSONNumber(policy["internal_scale"])) > .0001 {
				pending = append(pending, "Built-in display scale differs: "+name)
			}
		}
		reply, e := c.Run("hyprctl", "-j", "devices")
		if e != nil {
			return e
		}
		var devices map[string]any
		if e = json.Unmarshal([]byte(reply.Stdout), &devices); e != nil {
			return e
		}
		keyboards := map[string]map[string]any{}
		for _, k := range objects(devices["keyboards"]) {
			keyboards[str(k["name"])] = k
		}
		for _, name := range stringList(policy["internal_keyboards"]) {
			k := keyboards[name]
			layout, _ := k["layout"].(string)
			if layout == "" && policy["internal_keyboard"] == "hu" && strings.Contains(strings.ToLower(str(k["active_keymap"])), "hungarian") {
				layout = "hu"
			}
			if layout != str(policy["internal_keyboard"]) {
				pending = append(pending, "Built-in keyboard layout differs or is unavailable: "+name)
			}
		}
		for _, key := range []string{"input:natural_scroll", "input:touchpad:natural_scroll"} {
			reply, e = c.Run("hyprctl", "-j", "getoption", key)
			if e != nil {
				return e
			}
			var option map[string]any
			if e = json.Unmarshal([]byte(reply.Stdout), &option); e != nil {
				return e
			}
			expected := float64(0)
			if policy["natural_scroll"] == true {
				expected = 1
			}
			observed, present := option["int"].(float64)
			if !present || observed != expected {
				pending = append(pending, "Natural scrolling differs: "+key)
			}
		}
	}
	report["ready"] = len(pending) == 0
	report["pending"] = pending
	c.Report["laptop"] = report
	for _, warning := range pending {
		c.Warn(warning)
	}
	if len(pending) > 0 {
		return fmt.Errorf("laptop preferences have %d pending runtime checks", len(pending))
	}
	return nil
}
