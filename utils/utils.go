package utils

import (
	"net/url"
	"os"
	"strconv"
)

// GetEnv returns the value of the environment variable or the default value if not set
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvBool returns the value of the environment variable or the default value if not set
func GetEnvBool(key string, defaultValue bool) bool {
	envValue := GetEnv(key, strconv.FormatBool(defaultValue))
	value, err := strconv.ParseBool(envValue)
	if err != nil {
		return defaultValue
	}
	return value
}

// GetEnvInt returns the value of the environment variable or the default value if not set
func GetEnvInt(key string, defaultValue int) int {
	envValue := GetEnv(key, strconv.Itoa(defaultValue))
	value, err := strconv.Atoi(envValue)
	if err != nil {
		return defaultValue
	}
	return value
}

// StringToFloat64 converts a string to a float64
func StringToFloat64(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// GetQueryParam returns the value of the query parameter or the default value if not set
func GetQueryParam(query url.Values, key string, def ...string) string {
	value := query.Get(key)
	if value == "" {
		if len(def) > 0 {
			return def[0]
		}
		return ""
	}
	return value
}

// GetQueryParamBool returns the value of the query parameter or the default value if not set
func GetQueryParamBool(query url.Values, key string, def ...bool) bool {
	defValue := strconv.FormatBool(false)
	if len(def) > 0 {
		defValue = strconv.FormatBool(def[0])
	}
	value, err := strconv.ParseBool(GetQueryParam(query, key, defValue))
	if err != nil {
		value, err = strconv.ParseBool(defValue)
		if err != nil {
			return false
		}
	}
	return value
}
