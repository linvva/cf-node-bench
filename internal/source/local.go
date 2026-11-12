package source

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxLocalFileBytes  = 10 * 1024 * 1024
	maxLocalBatchBytes = 50 * 1024 * 1024
)

// ReadLocalFiles loads a one-time batch without retaining filesystem paths.
func ReadLocalFiles(ctx context.Context, paths []string, allowDomains bool) (ParseResult, error) {
	if len(paths) == 0 {
		return ParseResult{}, fmt.Errorf("请选择 CSV、TXT 或 JSON 文件")
	}
	result := ParseResult{}
	var total int
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return ParseResult{}, err
		}
		name := filepath.Base(path)
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".csv" && ext != ".txt" && ext != ".json" {
			return ParseResult{}, fmt.Errorf("%s：仅支持 CSV、TXT 或 JSON 文件", name)
		}
		data, err := readLocalFile(ctx, path)
		if err != nil {
			return ParseResult{}, fmt.Errorf("%s：%w", name, err)
		}
		total += len(data)
		if total > maxLocalBatchBytes {
			return ParseResult{}, fmt.Errorf("所选文件总大小不能超过 50 MiB")
		}
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(data) {
			return ParseResult{}, fmt.Errorf("%s：请使用 UTF-8 编码", name)
		}
		sourceID := "local:" + name
		var parsed ParseResult
		switch ext {
		case ".csv":
			parsed, err = parseLocalCSV(ctx, data, sourceID, allowDomains)
		case ".json":
			var decoded any
			if err = json.Unmarshal(data, &decoded); err == nil {
				walkJSON(decoded, sourceID, &parsed, allowDomains)
			}
		default:
			parsed = parse(data, sourceID, allowDomains)
		}
		if err != nil {
			return ParseResult{}, fmt.Errorf("%s：解析失败：%w", name, err)
		}
		if err := ctx.Err(); err != nil {
			return ParseResult{}, err
		}
		if len(parsed.Candidates) == 0 {
			return ParseResult{}, fmt.Errorf("%s：没有当前模式支持的有效节点", name)
		}
		result.Candidates = append(result.Candidates, parsed.Candidates...)
		result.Failures = append(result.Failures, parsed.Failures...)
	}
	return deduplicate(result), nil
}

func readLocalFile(ctx context.Context, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("请选择普通文件")
	}
	if info.Size() > maxLocalFileBytes {
		return nil, fmt.Errorf("单个文件不能超过 10 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(localContextReader{ctx, file}, maxLocalFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLocalFileBytes {
		return nil, fmt.Errorf("单个文件不能超过 10 MiB")
	}
	return data, nil
}

type localContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r localContextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func parseLocalCSV(ctx context.Context, data []byte, sourceID string, allowDomains bool) (ParseResult, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return ParseResult{}, fmt.Errorf("无法读取 CSV 表头：%w", err)
	}
	for i := range header {
		header[i] = strings.ToLower(strings.TrimSpace(header[i]))
	}
	column := func(names ...string) int {
		for _, name := range names {
			for i, value := range header {
				if value == name {
					return i
				}
			}
		}
		return -1
	}
	addressColumn := column("ip地址", "ip", "host", "address")
	if addressColumn < 0 {
		return ParseResult{}, fmt.Errorf("CSV 缺少入口地址列（IP地址、ip、host 或 address）")
	}
	portColumn := column("端口号", "port")
	countryColumn := column("ip位置", "country", "cc")
	result := ParseResult{}
	for {
		if err := ctx.Err(); err != nil {
			return ParseResult{}, err
		}
		row, err := reader.Read()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return ParseResult{}, err
		}
		port, country := 443, ""
		if portColumn >= 0 {
			value, err := strconv.Atoi(strings.TrimSpace(row[portColumn]))
			if err != nil {
				value = 0
			}
			port = value
		}
		if countryColumn >= 0 {
			country = row[countryColumn]
		}
		addCandidate(strings.TrimSpace(row[addressColumn]), port, country, sourceID, &result, allowDomains)
	}
}
