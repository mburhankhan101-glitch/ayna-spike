package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Tiny dotted-path reader over a decoded JSON document.
//
// The spike decodes vendor responses into map[string]any rather than typed
// structs on purpose: the field paths below are transcribed from published
// docs and are NOT yet verified against a live response. A typed struct would
// silently zero-fill on a mismatch; a path lookup can report "this path was
// not found", which is exactly the signal the spike exists to produce.
type doc map[string]any

func parseDoc(b []byte) (doc, error) {
	var d doc
	err := json.Unmarshal(b, &d)
	return d, err
}

// get walks "a.b.c" and also supports array indices as "a.0.b".
func (d doc) get(path string) (any, bool) {
	var cur any = map[string]any(d)
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// num returns a numeric field, reporting whether the path resolved.
func (d doc) num(path string) (float64, bool) {
	v, ok := d.get(path)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func (d doc) str(path string) (string, bool) {
	v, ok := d.get(path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// arrayLen is used to turn "list of detected acne polygons" into a count.
func (d doc) arrayLen(path string) (int, bool) {
	v, ok := d.get(path)
	if !ok {
		return 0, false
	}
	a, ok := v.([]any)
	if !ok {
		return 0, false
	}
	return len(a), true
}

func (d doc) present(path string) bool {
	_, ok := d.get(path)
	return ok
}
