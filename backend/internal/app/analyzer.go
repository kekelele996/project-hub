package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxZIPBytes         = 256 << 20
	maxArchiveBytes     = 1 << 30
	maxArchiveFileBytes = 32 << 20
	maxArchiveFileCount = 20000
	maxScreenshotBytes  = 12 << 20
)

type LanguageStat struct {
	Name       string  `json:"name"`
	Code       int     `json:"code"`
	Files      int     `json:"files"`
	Percentage float64 `json:"percentage"`
}

type archiveAnalysis struct {
	SourceFileCount int
	TotalFileCount  int
	Languages       []LanguageStat
	SHA256          string
	Size            int64
}

type sourceLanguage struct {
	name string
}

var sourceLanguages = map[string]sourceLanguage{
	".go": {name: "Go"}, ".java": {name: "Java"}, ".py": {name: "Python"},
	".js": {name: "JavaScript"}, ".jsx": {name: "JavaScript"}, ".mjs": {name: "JavaScript"}, ".cjs": {name: "JavaScript"},
	".ts": {name: "TypeScript"}, ".tsx": {name: "TypeScript"}, ".mts": {name: "TypeScript"}, ".cts": {name: "TypeScript"}, ".vue": {name: "Vue"}, ".svelte": {name: "Svelte"},
	".html": {name: "HTML"}, ".htm": {name: "HTML"}, ".css": {name: "CSS"}, ".scss": {name: "SCSS"}, ".less": {name: "Less"},
	".rs": {name: "Rust"}, ".c": {name: "C"}, ".h": {name: "C"}, ".cpp": {name: "C++"}, ".cc": {name: "C++"}, ".cxx": {name: "C++"}, ".hpp": {name: "C++"}, ".hxx": {name: "C++"},
	".cs": {name: "C#"}, ".php": {name: "PHP"}, ".rb": {name: "Ruby"}, ".swift": {name: "Swift"},
	".kt": {name: "Kotlin"}, ".kts": {name: "Kotlin"}, ".scala": {name: "Scala"}, ".dart": {name: "Dart"},
	".sql": {name: "SQL"}, ".sh": {name: "Shell"}, ".bash": {name: "Shell"}, ".zsh": {name: "Shell"},
	".r": {name: "R"}, ".lua": {name: "Lua"}, ".pl": {name: "Perl"},
	".ex": {name: "Elixir"}, ".exs": {name: "Elixir"}, ".erl": {name: "Erlang"},
	".clj": {name: "Clojure"}, ".fs": {name: "F#"}, ".m": {name: "Objective-C"}, ".mm": {name: "Objective-C"},
}

var ignoredDirectories = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "coverage": true,
	".next": true, ".nuxt": true, ".venv": true, "venv": true, "__pycache__": true, "target": true,
	".idea": true, ".vscode": true, "__macosx": true, "bin": true, "obj": true,
}

func sourceFileLanguage(name string) (sourceLanguage, bool) {
	parts := strings.Split(strings.ToLower(name), "/")
	for _, part := range parts[:len(parts)-1] {
		if ignoredDirectories[part] {
			return sourceLanguage{}, false
		}
	}
	base := parts[len(parts)-1]
	if strings.Contains(base, ".min.") || strings.HasSuffix(base, ".generated.go") || strings.HasSuffix(base, ".gen.go") || strings.HasSuffix(base, ".d.ts") {
		return sourceLanguage{}, false
	}
	lang, ok := sourceLanguages[strings.ToLower(path.Ext(name))]
	return lang, ok
}

func validateArchiveName(name string) error {
	if name == "" || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return errors.New("ZIP 包含不安全的文件路径")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return errors.New("ZIP 包含路径穿越文件")
		}
	}
	return nil
}

// Validate every archive entry; only source files reach an isolated temporary directory.
func analyzeArchive(filename string) (archiveAnalysis, error) {
	return analyzeArchiveContext(context.Background(), filename)
}

func analyzeArchiveContext(ctx context.Context, filename string) (archiveAnalysis, error) {
	out := archiveAnalysis{Languages: []LanguageStat{}}
	file, err := os.Open(filename)
	if err != nil {
		return out, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return out, err
	}
	if info.Size() > maxZIPBytes {
		return out, errors.New("ZIP 超过 256 MB 大小限制")
	}
	out.Size = info.Size()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return out, err
	}
	out.SHA256 = hex.EncodeToString(hash.Sum(nil))
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return out, errors.New("ZIP 文件格式无效")
	}
	if len(reader.File) > maxArchiveFileCount {
		return out, errors.New("ZIP 文件数量超过 20000 个")
	}
	var total uint64
	seen := make(map[string]bool)
	temporary, err := os.MkdirTemp("", "engineering-analysis-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(temporary)
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if err := validateArchiveName(entry.Name); err != nil {
			return out, err
		}
		name := path.Clean(entry.Name)
		if seen[name] {
			return out, fmt.Errorf("ZIP 包含重复路径：%s", name)
		}
		seen[name] = true
		if entry.Mode()&os.ModeSymlink != 0 || (!entry.FileInfo().IsDir() && !entry.Mode().IsRegular()) {
			return out, errors.New("ZIP 不允许符号链接或特殊文件")
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		out.TotalFileCount++
		if entry.UncompressedSize64 > maxArchiveFileBytes {
			return out, errors.New("ZIP 内单文件超过 32 MB 限制")
		}
		total += entry.UncompressedSize64
		if total > maxArchiveBytes || (entry.UncompressedSize64 > 1<<20 && entry.UncompressedSize64 > 200*entry.CompressedSize64) {
			return out, errors.New("ZIP 解压体积或压缩比例超过安全限制")
		}
		_, source := sourceFileLanguage(entry.Name)
		stream, err := entry.Open()
		if err != nil {
			return out, errors.New("ZIP 中存在无法读取的文件")
		}
		var copied int64
		if source {
			filename := filepath.Join(temporary, filepath.FromSlash(name))
			if err = os.MkdirAll(filepath.Dir(filename), 0700); err == nil {
				var destination *os.File
				destination, err = os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err == nil {
					copied, err = io.Copy(destination, io.LimitReader(stream, maxArchiveFileBytes+1))
					if closeErr := destination.Close(); err == nil {
						err = closeErr
					}
				}
			}
		} else {
			copied, err = io.Copy(io.Discard, io.LimitReader(stream, maxArchiveFileBytes+1))
		}
		closeErr := stream.Close()
		if err != nil || closeErr != nil || copied != int64(entry.UncompressedSize64) {
			return out, errors.New("ZIP 文件校验失败，文件可能损坏")
		}
	}
	stats, err := analyzeWithSCC(ctx, temporary)
	if err != nil {
		return out, err
	}
	codeTotal := 0
	for _, stat := range stats {
		codeTotal += stat.Code
		out.SourceFileCount += stat.Files
	}
	for _, stat := range stats {
		if codeTotal > 0 {
			stat.Percentage = float64(stat.Code) / float64(codeTotal) * 100
		}
		out.Languages = append(out.Languages, stat)
	}
	sort.Slice(out.Languages, func(i, j int) bool {
		if out.Languages[i].Code == out.Languages[j].Code {
			return out.Languages[i].Name < out.Languages[j].Name
		}
		return out.Languages[i].Code > out.Languages[j].Code
	})
	return out, nil
}

func analyzeWithSCC(parent context.Context, directory string) ([]LanguageStat, error) {
	binary := os.Getenv("SCC_BINARY")
	if binary == "" {
		binary = "scc"
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--format", "json", "--no-cocomo", "--no-complexity", "--no-gitignore", "--no-ignore", "--no-scc-ignore", "--no-gitmodule", "--no-min-gen", "--file-process-job-workers", "2", directory)
	output, err := command.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("未安装 scc 源码分析器，请配置 SCC_BINARY 后重试")
		}
		if ctx.Err() != nil {
			return nil, errors.New("源码分析超时，请减少 ZIP 文件体积")
		}
		return nil, errors.New("scc 源码分析失败")
	}
	var parsed []struct {
		Name        string
		Code, Count int
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, errors.New("scc 分析结果格式无效")
	}
	grouped := map[string]LanguageStat{}
	aliases := map[string]string{"JSX": "JavaScript", "C Header": "C", "C++ Header": "C++", "BASH": "Shell", "Zsh": "Shell", "LESS": "Less", "Sass": "SCSS", "Objective C": "Objective-C", "Objective C++": "Objective-C"}
	for _, item := range parsed {
		if item.Count > 0 {
			name := item.Name
			if alias, ok := aliases[name]; ok {
				name = alias
			}
			stat := grouped[name]
			stat.Name = name
			stat.Code += item.Code
			stat.Files += item.Count
			grouped[name] = stat
		}
	}
	stats := []LanguageStat{}
	for _, stat := range grouped {
		stats = append(stats, stat)
	}
	return stats, nil
}
