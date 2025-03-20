package utils

import (
	"net/url"
	"strconv"
)

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

func GetQueryParamBool(query url.Values, key string, def ...bool) bool {
	defValue := strconv.FormatBool(false)
	if len(def) > 0 {
		defValue = strconv.FormatBool(def[0])
	}
	value, err := strconv.ParseBool(GetQueryParam(query, key, defValue))
	if err != nil {
		value, err = strconv.ParseBool(defValue)
	}
	return value
}
