package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "fixture.zip")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestArchiveUsesEffectiveCodeAndExcludesDependencies(t *testing.T) {
	filename := archiveFixture(t, map[string]string{
		"project/main.go":                       "// comment\npackage main\n/* comment\n ends here */\nfunc main() {\n  println(\"https://github.com/*not a comment*/\") // comment\n}\n",
		"project/app.py":                        "# comment\n\nvalue = '# not a comment'\nprint(value) # comment\n",
		"project/node_modules/package/index.js": "alert(1)\n",
		"project/vendor/mod/file.go":            "package ignored\n",
		"project/.git/hooks/a.py":               "print(1)\n",
		"project/main.gen.go":                   "package generated\n",
		"project/README.md":                     "# Read me\n",
		"project/package.json":                  "{}\n",
	})
	result, err := analyzeArchive(filename)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalFileCount != 8 || result.SourceFileCount != 2 {
		t.Fatalf("file counts: %#v", result)
	}
	counts := map[string]int{}
	percent := 0.0
	for _, language := range result.Languages {
		counts[language.Name] = language.Code
		percent += language.Percentage
	}
	if counts["Go"] != 4 || counts["Python"] != 2 {
		t.Fatalf("effective code count: %#v", counts)
	}
	if math.Abs(percent-100) > 0.000001 {
		t.Fatalf("percentages sum to %f", percent)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	if result.SHA256 != hex.EncodeToString(hash[:]) || result.Size != int64(len(data)) {
		t.Fatal("archive digest or size is incorrect")
	}
}

func TestArchiveRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"../secret.go", "project/../../secret.go", "/tmp/main.go", `project\main.go`, "C:/main.go"} {
		t.Run(name, func(t *testing.T) {
			filename := archiveFixture(t, map[string]string{name: "package main\n"})
			if _, err := analyzeArchive(filename); err == nil {
				t.Fatal("expected unsafe path to fail")
			}
		})
	}
}

func TestArchiveGroupsLanguageVariants(t *testing.T) {
	filename := archiveFixture(t, map[string]string{
		"app.js":   "console.log('hello')\n",
		"view.jsx": "export const View = () => <div>Hello</div>\n",
		"source.c": "int value = 1;\n",
		"source.h": "int value;\n",
	})
	result, err := analyzeArchive(filename)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceFileCount != 4 || len(result.Languages) != 2 {
		t.Fatalf("unexpected language grouping: %#v", result)
	}
	for _, stat := range result.Languages {
		if (stat.Name != "JavaScript" && stat.Name != "C") || stat.Code != 2 || stat.Files != 2 || stat.Percentage != 50 {
			t.Fatalf("unexpected language statistics: %#v", stat)
		}
	}
}

func TestArchiveRejectsZIPBomb(t *testing.T) {
	filename := archiveFixture(t, map[string]string{"huge.go": strings.Repeat("0", 2<<20)})
	if _, err := analyzeArchive(filename); err == nil {
		t.Fatal("expected compression bomb to fail")
	}
}

func TestArchiveRejectsSymlinkAndDuplicate(t *testing.T) {
	for _, symlink := range []bool{true, false} {
		filename := filepath.Join(t.TempDir(), "fixture.zip")
		file, err := os.Create(filename)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		for i := 0; i < 2; i++ {
			header := &zip.FileHeader{Name: "main.go", Method: zip.Store}
			if symlink {
				header.SetMode(os.ModeSymlink | 0777)
			}
			entry, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = entry.Write([]byte("package main\n"))
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := analyzeArchive(filename); err == nil {
			t.Fatal("expected symlink or duplicate path to fail")
		}
	}
}

func TestArchiveRejectsCorruptCRC(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "fixture.zip")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "main.go", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("package main\n"))
	_ = writer.Close()
	_ = file.Close()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	index := bytes.Index(data, []byte("package main"))
	if index < 0 {
		t.Fatal("fixture content missing")
	}
	data[index] = 'X'
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := analyzeArchive(filename); err == nil {
		t.Fatal("expected corrupt CRC to fail")
	}
}

func TestArchiveDoesNotExecuteSource(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "must-not-exist")
	filename := archiveFixture(t, map[string]string{"run.sh": "#!/bin/sh\ntouch " + sentinel + "\n"})
	if _, err := analyzeArchive(filename); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("archive source was executed")
	}
}

func TestScreenshotValidatesContent(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.image")
	file, err := os.Create(valid)
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 4))
	canvas.Set(0, 0, color.RGBA{R: 100, A: 255})
	if err := png.Encode(file, canvas); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if mime, err := validateScreenshot(valid); err != nil || mime != "image/png" {
		t.Fatalf("valid PNG rejected: %s %v", mime, err)
	}
	invalid := filepath.Join(dir, "invalid.png")
	if err := os.WriteFile(invalid, []byte("<svg onload='alert(1)'></svg>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateScreenshot(invalid); err == nil {
		t.Fatal("expected non-raster content rejection")
	}
	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid, data[:len(data)/2], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateScreenshot(invalid); err == nil {
		t.Fatal("expected corrupted PNG rejection")
	}
}

func TestScreenshotAcceptsWebP(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8Oj3/yC5YXXI1/8gP+QH/ID/+PIAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "fixture.image")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	if mime, err := validateScreenshot(filename); err != nil || mime != "image/webp" {
		t.Fatalf("valid WebP rejected: %s %v", mime, err)
	}
}

func TestTechStackQuery(t *testing.T) {
	values := techStackQuery([]string{" Go, TypeScript ", "Go", "全部", "all", "Python"})
	if strings.Join(values, ",") != "go,typescript,python" {
		t.Fatalf("unexpected technology filter: %v", values)
	}
}

func TestStoredPathRejectsEscapes(t *testing.T) {
	app := &App{UploadDir: t.TempDir()}
	for _, name := range []string{"../secret", "/tmp/secret", ""} {
		if _, err := app.storedPath(name); err == nil {
			t.Fatalf("path %q should be rejected", name)
		}
	}
	if _, err := app.storedPath("versions/123/file.zip"); err != nil {
		t.Fatal(err)
	}
}
