package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultEnvFile = ".env"

type configDefaults struct {
	port         int
	cidr         string
	username     string
	password     string
	dialParallel int
	dialTimeout  int
	dnsCacheTTL  int
}

func loadConfigDefaults() (configDefaults, error) {
	defaults := configDefaults{
		port:         52122,
		cidr:         "",
		username:     "",
		password:     "",
		dialParallel: 5,
		dialTimeout:  3000,
		dnsCacheTTL:  30,
	}

	return loadDotEnvConfig(defaultEnvFile, defaults)
}

func loadDotEnvConfig(path string, defaults configDefaults) (configDefaults, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaults, nil
		}
		return configDefaults{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	config := defaults
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			return configDefaults{}, fmt.Errorf("%s:%d invalid line: missing '='", path, lineNo)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return configDefaults{}, fmt.Errorf("%s:%d invalid line: empty key", path, lineNo)
		}

		parsedValue, err := parseDotEnvValue(strings.TrimSpace(value))
		if err != nil {
			return configDefaults{}, fmt.Errorf("%s:%d invalid value for %s: %w", path, lineNo, key, err)
		}

		switch key {
		case "PORT":
			port, err := parseEnvPort(parsedValue, defaults.port)
			if err != nil {
				return configDefaults{}, fmt.Errorf("%s:%d invalid PORT: %w", path, lineNo, err)
			}
			config.port = port
		case "CIDR":
			config.cidr = parsedValue
		case "PROXY_USERNAME":
			config.username = parsedValue
		case "PROXY_PASSWORD":
			config.password = parsedValue
		case "DIAL_PARALLELISM":
			dialParallel, err := parsePositiveInt(parsedValue, defaults.dialParallel)
			if err != nil {
				return configDefaults{}, fmt.Errorf("%s:%d invalid DIAL_PARALLELISM: %w", path, lineNo, err)
			}
			config.dialParallel = dialParallel
		case "DIAL_TIMEOUT_MS":
			dialTimeout, err := parsePositiveInt(parsedValue, defaults.dialTimeout)
			if err != nil {
				return configDefaults{}, fmt.Errorf("%s:%d invalid DIAL_TIMEOUT_MS: %w", path, lineNo, err)
			}
			config.dialTimeout = dialTimeout
		case "DNS_CACHE_TTL_SECONDS":
			dnsCacheTTL, err := parseNonNegativeInt(parsedValue, defaults.dnsCacheTTL)
			if err != nil {
				return configDefaults{}, fmt.Errorf("%s:%d invalid DNS_CACHE_TTL_SECONDS: %w", path, lineNo, err)
			}
			config.dnsCacheTTL = dnsCacheTTL
		}
	}

	if err := scanner.Err(); err != nil {
		return configDefaults{}, fmt.Errorf("read %s: %w", path, err)
	}
	return config, nil
}

func parseDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return "", err
		}
		return unquoted, nil
	}

	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}

	return raw, nil
}

func parseEnvPort(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("must be greater than 0")
	}
	return parsed, nil
}

func parseNonNegativeInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("must be greater than or equal to 0")
	}
	return parsed, nil
}
