package source

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/linvva/cf-node-bench/internal/model"
)

type ParseFailure struct {
	Value  string              `json:"value"`
	Reason model.FailureReason `json:"reason"`
}

type ParseResult struct {
	Candidates []model.Candidate `json:"candidates"`
	Failures   []ParseFailure    `json:"failures"`
}

func Parse(data []byte, sourceID string) ParseResult {
	var decoded any
	if json.Unmarshal(data, &decoded) == nil {
		result := ParseResult{}
		walkJSON(decoded, sourceID, &result)
		return deduplicate(result)
	}
	result := ParseResult{}
	for _, token := range strings.FieldsFunc(string(data), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	}) {
		parseToken(token, sourceID, &result)
	}
	return deduplicate(result)
}

func walkJSON(value any, sourceID string, result *ParseResult) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			walkJSON(item, sourceID, result)
		}
	case map[string]any:
		ip, hasIP := stringValue(typed, "ip", "host")
		port, hasPort := intValue(typed, "port")
		if hasIP || hasPort {
			country, _ := stringValue(typed, "country", "cc")
			addCandidate(ip, port, country, sourceID, result)
			return
		}
		for _, child := range typed {
			walkJSON(child, sourceID, result)
		}
	case string:
		for _, token := range strings.Fields(typed) {
			parseToken(token, sourceID, result)
		}
	}
}

func parseToken(token, sourceID string, result *ParseResult) {
	address, country, _ := strings.Cut(strings.TrimSpace(token), "#")
	if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
		addCandidate(address, 443, country, sourceID, result)
		return
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		result.Failures = append(result.Failures, ParseFailure{token, model.FailureInvalidPort})
		return
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		port = 0
	}
	addCandidate(host, port, country, sourceID, result)
}

func addCandidate(ipText string, port int, country, sourceID string, result *ParseResult) {
	ip := net.ParseIP(strings.TrimSpace(ipText))
	value := fmt.Sprintf("%s:%d", ipText, port)
	if ip == nil || ip.To4() == nil {
		result.Failures = append(result.Failures, ParseFailure{value, model.FailureInvalidIP})
		return
	}
	if port < 1 || port > 65535 {
		result.Failures = append(result.Failures, ParseFailure{value, model.FailureInvalidPort})
		return
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	if country != "" && (len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z') {
		result.Failures = append(result.Failures, ParseFailure{value, model.FailureInvalidTag})
		return
	}
	result.Candidates = append(result.Candidates, model.Candidate{AddressType: model.AddressIPv4, IP: ip.String(), Port: port, Country: country, SourceID: sourceID})
}

func stringValue(object map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			switch typed := value.(type) {
			case string:
				return typed, true
			case json.Number:
				return typed.String(), true
			}
		}
	}
	return "", false
}

func intValue(object map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			switch typed := value.(type) {
			case float64:
				return int(typed), true
			case string:
				parsed, err := strconv.Atoi(typed)
				return parsed, err == nil
			}
		}
	}
	return 0, false
}

func deduplicate(result ParseResult) ParseResult {
	seen := map[string]bool{}
	filtered := result.Candidates[:0]
	for _, candidate := range result.Candidates {
		if !seen[candidate.Key()] {
			seen[candidate.Key()] = true
			filtered = append(filtered, candidate)
		}
	}
	result.Candidates = filtered
	return result
}
