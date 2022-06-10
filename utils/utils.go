package utils

import (
	"regexp"
	"strings"
)

func SnakeCase(s string) string {
	s = strings.Title(s)
	re := regexp.MustCompile(`[A-Z]+[^A-Z\.]*`)
	split := re.FindAllString(s, -1)
	for n, s := range split {
		s = strings.ToLower(s)
		split[n] = strings.TrimSuffix(s, "_")
	}
	return strings.Join(split, "_")
}

func Contains(arr []string, s string) bool {
	for _, a := range arr {
		if s == a {
			return true
		}
	}
	return false
}
