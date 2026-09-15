// Package env loads KEY=VALUE settings from a .env file into the process
// environment and provides a small accessor with defaults.
package env

import (
	"bufio"
	"os"
	"strings"
)

// Load reads .env from the current working directory and puts each
// KEY=VALUE pair into the process environment. Existing OS environment
// variables win and are never overwritten. A missing .env file is not an
// error.
func Load() error {
	f, err := os.Open(".env")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return sc.Err()
}

// Get returns the value of environment variable key, falling back to def
// when the variable is unset or empty.
func Get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
