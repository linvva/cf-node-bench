package source

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/linvva/cf-node-bench/internal/config"
	"github.com/linvva/cf-node-bench/internal/model"
	"golang.org/x/net/idna"
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
	return parse(data, sourceID, false)
}

func ParseH3(data []byte, sourceID string) ParseResult {
	return parse(data, sourceID, true)
}

func parse(data []byte, sourceID string, allowDomains bool) ParseResult {
	var decoded any
	if json.Unmarshal(data, &decoded) == nil {
		result := ParseResult{}
		walkJSON(decoded, sourceID, &result, allowDomains)
		return deduplicate(result)
	}
	result := ParseResult{}
	for _, token := range strings.FieldsFunc(string(data), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	}) {
		parseToken(token, sourceID, &result, allowDomains)
	}
	return deduplicate(result)
}

func walkJSON(value any, sourceID string, result *ParseResult, allowDomains bool) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			walkJSON(item, sourceID, result, allowDomains)
		}
	case map[string]any:
		ip, hasIP := stringValue(typed, "ip", "host")
		port, hasPort := intValue(typed, "port")
		if hasIP || hasPort {
			if allowDomains && !hasPort {
				if _, specified := typed["port"]; !specified {
					port = 443
				}
			}
			country, _ := stringValue(typed, "country", "cc")
			addCandidate(ip, port, country, sourceID, result, allowDomains)
			return
		}
		for _, child := range typed {
			walkJSON(child, sourceID, result, allowDomains)
		}
	case string:
		for _, token := range strings.Fields(typed) {
			parseToken(token, sourceID, result, allowDomains)
		}
	}
}

func parseToken(token, sourceID string, result *ParseResult, allowDomains bool) {
	address, country, _ := strings.Cut(strings.TrimSpace(token), "#")
	if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
		addCandidate(address, 443, country, sourceID, result, allowDomains)
		return
	}
	if allowDomains && !strings.Contains(address, ":") {
		addCandidate(address, 443, country, sourceID, result, true)
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
	addCandidate(host, port, country, sourceID, result, allowDomains)
}

func addCandidate(ipText string, port int, country, sourceID string, result *ParseResult, allowDomains bool) {
	ip := net.ParseIP(strings.TrimSpace(ipText))
	value := fmt.Sprintf("%s:%d", ipText, port)
	host, validHost := normalizeDomain(ipText)
	if (ip == nil || ip.To4() == nil) && !(allowDomains && ip == nil && validHost) {
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
	candidate := model.Candidate{AddressType: model.AddressIPv4, Port: port, Country: country, SourceID: sourceID}
	if ip != nil {
		candidate.IP = ip.String()
	} else {
		candidate.AddressType = model.AddressDomain
		candidate.Host = host
	}
	result.Candidates = append(result.Candidates, candidate)
}

func normalizeDomain(raw string) (string, bool) {
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), "."))
	if err != nil || !config.ValidDomain(host) {
		return "", false
	}
	// An invalid numeric IPv4 must not silently become a domain candidate.
	if strings.Trim(host, "0123456789.") == "" {
		return "", false
	}
	return host, true
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
