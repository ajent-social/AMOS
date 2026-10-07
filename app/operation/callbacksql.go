package operation

import "strings"

// callbackSQL accepts a deliberately finite byte-level SQL profile. It is not
// a PostgreSQL parser or a sandbox for trusted application handlers.
func callbackSQL(q string) bool {
	if len(q) == 0 || len(q) > 65536 {
		return false
	}
	for i := range len(q) {
		c := q[i]
		if c >= 127 || c == '\\' || (c < 32 && !callbackSpace(c)) {
			return false
		}
	}
	first, ended, depth := false, false, 0
	for i := 0; i < len(q); {
		c := q[i]
		if callbackSpace(c) {
			i++
			continue
		}
		if i+1 < len(q) && q[i:i+2] == "--" {
			i += 2
			for i < len(q) && q[i] != '\r' && q[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(q) && q[i:i+2] == "/*" {
			i += 2
			nesting := 1
			for i < len(q) && nesting > 0 {
				if i+1 < len(q) && q[i:i+2] == "/*" {
					nesting++
					if nesting > 32 {
						return false
					}
					i += 2
				} else if i+1 < len(q) && q[i:i+2] == "*/" {
					nesting--
					i += 2
				} else {
					i++
				}
			}
			if nesting != 0 {
				return false
			}
			continue
		}
		if ended {
			return false
		}
		if callbackLetter(c) {
			start := i
			i++
			for i < len(q) && callbackIdent(q[i]) {
				i++
			}
			token := strings.ToUpper(q[start:i])
			if !first {
				if token != "SELECT" && token != "INSERT" && token != "UPDATE" && token != "DELETE" {
					return false
				}
				first = true
			}
			if token == "UESCAPE" || (token == "U" && i < len(q) && q[i] == '&') {
				return false
			}
			if i < len(q) && q[i] == '\'' && (token == "E" || token == "B" || token == "X" || token == "N") {
				return false
			}
			continue
		}
		if !first {
			return false
		}
		switch c {
		case '\'', '"':
			i++
			closed := false
			for i < len(q) {
				if q[i] != c {
					i++
					continue
				}
				i++
				if i < len(q) && q[i] == c {
					i++
					continue
				}
				closed = true
				break
			}
			if !closed {
				return false
			}
		case '$':
			if i > 0 && (callbackIdent(q[i-1]) || q[i-1] == '$') {
				return false
			}
			i++
			if i == len(q) || q[i] < '1' || q[i] > '9' {
				return false
			}
			for i < len(q) && q[i] >= '0' && q[i] <= '9' {
				i++
			}
			if i < len(q) && (callbackIdent(q[i]) || q[i] == '$') {
				return false
			}
		case '(':
			depth++
			if depth > 64 {
				return false
			}
			i++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
			i++
		case ';':
			if depth != 0 {
				return false
			}
			ended = true
			i++
		default:
			i++
		}
	}
	return first && depth == 0
}

func callbackSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func callbackLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func callbackIdent(c byte) bool {
	return callbackLetter(c) || c >= '0' && c <= '9'
}
