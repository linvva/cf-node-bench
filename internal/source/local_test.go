package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linvva/cf-node-bench/internal/model"
)

func localFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadLocalCSVUsesEntranceColumnsAndKeepsPorts(t *testing.T) {
	dir := t.TempDir()
	path := localFixture(t, dir, "入口.csv", "\ufeffIP数据来源,IP地址,端口号,TLS,IP位置,出站IP,网络延迟,HTTP版本\n"+
		"\"Example, Inc.\",192.0.2.1,8443,true,HK,198.51.100.8,0 ms,http/1.1\n"+
		"example,192.0.2.1,443,true,JP,2001:db8::1,0 ms,http/1.1\n"+
		"example,192.0.2.2,,true,US,198.51.100.9,0 ms,http/1.1\n")
	for _, allowDomains := range []bool{false, true} {
		result, err := ReadLocalFiles(t.Context(), []string{path}, allowDomains)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Candidates) != 2 || result.Candidates[0].IP != "192.0.2.1" || result.Candidates[0].Port != 8443 || result.Candidates[0].Country != "HK" || result.Candidates[1].Port != 443 {
			t.Fatalf("wrong entrance mapping: %+v", result.Candidates)
		}
		if len(result.Failures) != 1 || result.Failures[0].Reason != model.FailureInvalidPort {
			t.Fatalf("empty port was not rejected: %+v", result.Failures)
		}
		if result.Candidates[0].SourceID != "local:入口.csv" {
			t.Fatal("source identifier should contain only the filename")
		}
	}
}

func TestReadLocalFilesMergesFormatsAndRespectsDomainMode(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		localFixture(t, dir, "nodes.TXT", "\ufeff192.0.2.1:8443#HK\n192.0.2.2\n"),
		localFixture(t, dir, "nodes.json", "\ufeff[{\"ip\":\"192.0.2.1\",\"port\":8443},{\"host\":\"EDGE.EXAMPLE.COM\",\"port\":443,\"cc\":\"jp\"}]"),
		localFixture(t, dir, "nodes.csv", "Address,cc\nedge.example.com,JP\n192.0.2.3,US\n"),
	}
	for _, allowDomains := range []bool{false, true} {
		result, err := ReadLocalFiles(t.Context(), paths, allowDomains)
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if allowDomains {
			want = 4
		}
		if len(result.Candidates) != want {
			t.Fatalf("allowDomains=%v candidates=%+v", allowDomains, result.Candidates)
		}
		if allowDomains && (result.Candidates[2].AddressType != model.AddressDomain || result.Candidates[2].Host != "edge.example.com") {
			t.Fatal("H3 domain identity was not retained")
		}
	}
}

func TestReadLocalFilesRejectsWholeBatchForInvalidFile(t *testing.T) {
	dir := t.TempDir()
	valid := localFixture(t, dir, "valid.txt", "192.0.2.1:443")
	for _, test := range []struct{ name, content, want string }{
		{"wrong.csv", "出站IP,端口号\n198.51.100.1,443\n", "入口地址列"},
		{"broken.csv", "IP地址,端口号\n192.0.2.1,443\n\"192.0.2.2,443\n", "解析失败"},
		{"broken.json", "[192.0.2.1]", "解析失败"},
		{"invalid.csv", "ip,port\n192.0.2.1,99999\n", "没有当前模式支持的有效节点"},
		{"empty.txt", "", "没有当前模式支持的有效节点"},
		{"domain.txt", "edge.example.com", "没有当前模式支持的有效节点"},
		{"encoding.txt", "\xff192.0.2.1", "UTF-8"},
		{"archive.zip", "192.0.2.1", "仅支持"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := localFixture(t, dir, test.name, test.content)
			result, err := ReadLocalFiles(t.Context(), []string{valid, path}, false)
			if err == nil || !strings.Contains(err.Error(), test.want) || len(result.Candidates) != 0 {
				t.Fatalf("partial batch was accepted or wrong error: %+v, %v", result, err)
			}
		})
	}
	for _, paths := range [][]string{nil, {filepath.Join(dir, "missing.csv")}, {localDirectory(t, dir)}} {
		if _, err := ReadLocalFiles(t.Context(), paths, false); err == nil {
			t.Fatalf("invalid file selection accepted: %v", paths)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadLocalFiles(ctx, []string{valid}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read returned %v", err)
	}
}

func localDirectory(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "directory.csv")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadLocalFilesRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxLocalFileBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, err := ReadLocalFiles(t.Context(), []string{path}, false); err == nil || !strings.Contains(err.Error(), "10 MiB") {
		t.Fatalf("oversized file was not rejected: %v", err)
	}
}

func TestReadLocalFilesRejectsOversizedBatch(t *testing.T) {
	dir := t.TempDir()
	content := "192.0.2.1" + strings.Repeat(" ", maxLocalFileBytes-len("192.0.2.1"))
	var paths []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt", "f.txt"} {
		paths = append(paths, localFixture(t, dir, name, content))
	}
	result, err := ReadLocalFiles(t.Context(), paths, false)
	if err == nil || !strings.Contains(err.Error(), "50 MiB") || len(result.Candidates) != 0 {
		t.Fatalf("oversized batch was not rejected atomically: %+v, %v", result, err)
	}
}
