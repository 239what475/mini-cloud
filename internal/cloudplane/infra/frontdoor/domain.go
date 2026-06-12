package frontdoor

import (
	"strings"
)

func trimCNAMEValue(value *string) string {
	if value == nil {
		return ""
	}
	return trimCNAME(*value)
}

func trimCNAME(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}
