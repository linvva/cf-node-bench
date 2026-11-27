package region

import (
	_ "embed"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const regionalIndicatorA = '\U0001F1E6'

//go:embed 国家与地区.json
var namesJSON []byte

var names = func() map[string]string {
	var result map[string]string
	if err := json.Unmarshal(namesJSON, &result); err != nil {
		panic(err)
	}
	return result
}()

func Label(country string) string {
	code := strings.ToUpper(strings.TrimSpace(country))
	if name, ok := names[code]; ok {
		flag := string([]rune{regionalIndicatorA + rune(code[0]-'A'), regionalIndicatorA + rune(code[1]-'A')})
		return flag + name
	}
	return code
}

// CodeFromFlag reads the leading pair of regional indicators in a text label.
func CodeFromFlag(label string) (string, bool) {
	first, width := utf8.DecodeRuneInString(label)
	second, _ := utf8.DecodeRuneInString(label[width:])
	if first < regionalIndicatorA || first >= regionalIndicatorA+26 ||
		second < regionalIndicatorA || second >= regionalIndicatorA+26 {
		return "", false
	}
	return string([]byte{'A' + byte(first-regionalIndicatorA), 'A' + byte(second-regionalIndicatorA)}), true
}
