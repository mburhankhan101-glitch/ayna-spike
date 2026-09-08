package main

import "strings"

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func normaliseLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// siblingPath swaps the final dotted segment: "a.b.score" -> "a.b.confidence".
// A path with no dot is returned unchanged rather than silently becoming a
// bare field name that would match the wrong thing.
func siblingPath(path, sibling string) string {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return path
	}
	return path[:i+1] + sibling
}

// providerNames lists arm names for the -only error message, so a typo tells
// you what was actually available rather than just that you were wrong.
func providerNames(ps []Provider) string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name())
	}
	return strings.Join(names, ", ")
}
