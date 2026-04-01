package utils

import (
	"strings"
	"time"
)

// SplitName splits a full name into first and last name
func SplitName(full string) (first, last string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], parts[len(parts)-1]
	}
}

// FormatAddress combines address components into a single formatted string
func FormatAddress(street, city, state, zip string) string {
	var parts []string
	if street != "" {
		parts = append(parts, street)
	}
	var cityStateZip string
	if city != "" && state != "" {
		cityStateZip = city + ", " + state
	} else if city != "" {
		cityStateZip = city
	} else if state != "" {
		cityStateZip = state
	}
	if zip != "" {
		if cityStateZip != "" {
			cityStateZip += " " + zip
		} else {
			cityStateZip = zip
		}
	}
	if cityStateZip != "" {
		parts = append(parts, cityStateZip)
	}
	return strings.Join(parts, ", ")
}

// ColumnIndex finds the index of a column by name (case-insensitive)
func ColumnIndex(headers []string, name string) int {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	for i, h := range headers {
		if strings.ToLower(strings.TrimSpace(h)) == nameLower {
			return i
		}
	}
	return -1
}

// OutputFilename generates a timestamped output filename
func OutputFilename() string {
	return "output_" + time.Now().Format("20060102_150405") + ".csv"
}
