package daemon

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestListWorkspaceWallpaperCandidatesFiltersSupportedFiles(t *testing.T) {
	dir := t.TempDir()

	files := []string{
		"alpha.jpg",
		"beta.png",
		"gamma.jpeg",
		".DS_Store",
		"notes.txt",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	got, err := listWorkspaceWallpaperCandidates(dir)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}

	want := []string{
		filepath.Join(dir, "alpha.jpg"),
		filepath.Join(dir, "beta.png"),
		filepath.Join(dir, "gamma.jpeg"),
	}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRenderWorkspaceWallpaperCreatesJPEG(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.png")
	outputPath := filepath.Join(dir, "workspace-4.jpg")

	src := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1600; x++ {
			src.Set(x, y, color.RGBA{R: 28, G: 54, B: 76, A: 255})
		}
	}

	sourceFile, err := os.Create(sourcePath)
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := png.Encode(sourceFile, src); err != nil {
		sourceFile.Close()
		t.Fatalf("encode source png: %v", err)
	}
	if err := sourceFile.Close(); err != nil {
		t.Fatalf("close source file: %v", err)
	}

	if err := RenderWorkspaceWallpaper(sourcePath, outputPath, "Deep Work"); err != nil {
		t.Fatalf("render wallpaper: %v", err)
	}

	outputFile, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer outputFile.Close()

	rendered, _, err := image.Decode(outputFile)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if rendered.Bounds().Dx() != 1600 || rendered.Bounds().Dy() != 900 {
		t.Fatalf("output bounds = %v, want 1600x900", rendered.Bounds())
	}
}
