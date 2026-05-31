package master

import (
	"errors"
	"regexp"
	"strings"
)

var subdomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeInstanceName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !subdomainPattern.MatchString(name) {
		return "", errors.New("name must be a valid DNS label")
	}
	if name == "www" || name == "api" || name == "admin" || name == "_" {
		return "", errors.New("name is reserved")
	}
	return name, nil
}
