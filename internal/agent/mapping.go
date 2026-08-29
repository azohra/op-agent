package agent

import (
	"bufio"
	"fmt"
	"sort"
	"strings"
)

type Mapping map[string]string

func ParseMapping(input string) (Mapping, error) {
	mapping := Mapping{}
	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 1024), 1<<20)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("mapping line %d must be NAME op://reference", lineNumber)
		}
		name, ref := fields[0], fields[1]
		if !envNamePattern.MatchString(name) {
			return nil, fmt.Errorf("mapping line %d has invalid environment name %q", lineNumber, name)
		}
		if !strings.HasPrefix(ref, "op://") || len(ref) == len("op://") {
			return nil, fmt.Errorf("mapping line %d has invalid 1Password reference", lineNumber)
		}
		if existing, ok := mapping[name]; ok && existing != ref {
			return nil, fmt.Errorf("mapping line %d conflicts with the earlier value for %s", lineNumber, name)
		}
		mapping[name] = ref
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mapping: %w", err)
	}
	return mapping, nil
}

func MappingForProfile(profile string, lookup func(string) string) (Mapping, error) {
	base, err := ParseMapping(lookup("OP_AGENT_REFS"))
	if err != nil {
		return nil, fmt.Errorf("OP_AGENT_REFS: %w", err)
	}
	if profile == "" {
		return base, nil
	}
	profileEnvironment, err := profileEnvironmentName(profile)
	if err != nil {
		return nil, err
	}
	overlayInput := lookup(profileEnvironment)
	if strings.TrimSpace(overlayInput) == "" {
		return nil, fmt.Errorf("profile %q requires a non-empty %s mapping", profile, profileEnvironment)
	}
	overlay, err := ParseMapping(overlayInput)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", profileEnvironment, err)
	}
	for name, ref := range overlay {
		base[name] = ref
	}
	return base, nil
}

func profileEnvironmentName(profile string) (string, error) {
	if !profilePattern.MatchString(profile) {
		return "", fmt.Errorf("profile must contain 1-32 lowercase letters, numbers, or hyphens and start with a letter")
	}
	suffix := strings.ToUpper(strings.ReplaceAll(profile, "-", "_"))
	return "OP_AGENT_REFS_" + suffix, nil
}

func SelectMapping(mapping Mapping, keys []string) (Mapping, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("at least one key is required")
	}
	selected := make(Mapping, len(keys))
	seen := map[string]bool{}
	for _, raw := range keys {
		name := strings.TrimSpace(raw)
		if name == "" || !envNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid environment key %q", raw)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		ref, ok := mapping[name]
		if !ok {
			return nil, fmt.Errorf("no 1Password reference is defined for %s", name)
		}
		selected[name] = ref
	}
	return selected, nil
}

func splitKeys(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
