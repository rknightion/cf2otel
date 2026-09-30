package aigateway

import "strings"

// normalizeModel validates identifier syntax, not membership in a model catalog:
// provider catalogs evolve independently of the collector. A leading @ denotes
// a model namespace (for example Workers AI), rather than a provider prefix.
func normalizeModel(raw string) string {
	if !validModelIdentifier(raw) {
		return "unknown"
	}
	model := raw
	if !strings.HasPrefix(model, "@") {
		if _, suffix, prefixed := strings.Cut(model, "/"); prefixed {
			model = suffix
		}
	}
	switch strings.ToLower(model) {
	case "null", "none", "undefined", "unknown":
		return "unknown"
	default:
		return model
	}
}

func validModelIdentifier(model string) bool {
	for _, segment := range strings.Split(model, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		hasAlphanumeric := false
		for _, c := range segment {
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
				hasAlphanumeric = true
			case c == '-', c == '_', c == '.', c == ':', c == '@':
			default:
				return false
			}
		}
		if !hasAlphanumeric {
			return false
		}
	}
	return true
}
