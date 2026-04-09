package daemon

import (
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"autohide/config"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	workspaceWallpaperOutputDirName   = "workspace-wallpapers"
	workspaceWallpaperSwitchDelay     = 250 * time.Millisecond
	workspaceWallpaperApplyDelay      = 350 * time.Millisecond
	minWorkspaceWallpaperFontSize     = 28.0
	maxWorkspaceWallpaperFontSize     = 112.0
	workspaceWallpaperWidthRatio      = 0.42
	workspaceWallpaperFontSizeRatio   = 0.042
	defaultWorkspaceWallpaperSubdir   = "Desktop/3-Resources/wallpapers"
	workspaceWallpaperJPEGQuality     = 92
	workspaceWallpaperBoxAlpha        = 168
	workspaceWallpaperTextAlpha       = 244
	workspaceWallpaperTextShadowAlpha = 120
)

var (
	workspaceWallpaperFontOnce sync.Once
	workspaceWallpaperFont     *opentype.Font
	workspaceWallpaperFontErr  error
)

func UpdateWorkspaceLabel(cfg *config.Config, cfgPath string, ws int, label string) error {
	if ws < 1 {
		return fmt.Errorf("workspace number must be positive")
	}

	label = NormalizeWorkspaceLabel(label)
	if cfg.Workspaces == nil {
		cfg.Workspaces = make(map[string]string)
	}

	key := strconv.Itoa(ws)
	if label == "" {
		delete(cfg.Workspaces, key)
	} else {
		cfg.Workspaces[key] = label
	}

	if err := config.Save(cfg, cfgPath); err != nil {
		return err
	}
	if err := refreshWorkspaceWallpaper(ws, label); err != nil {
		return fmt.Errorf("workspace %d label saved but wallpaper update failed: %w", ws, err)
	}
	return nil
}

func refreshWorkspaceWallpaper(ws int, label string) error {
	sourceDir, err := workspaceWallpaperSourceDir()
	if err != nil {
		return err
	}
	candidates, err := listWorkspaceWallpaperCandidates(sourceDir)
	if err != nil {
		return err
	}
	sourcePath, err := chooseRandomWallpaper(candidates)
	if err != nil {
		return err
	}

	wallpaperPath := sourcePath
	if label != "" {
		wallpaperPath = workspaceWallpaperOutputPath(ws)
		if err := RenderWorkspaceWallpaper(sourcePath, wallpaperPath, label); err != nil {
			return err
		}
	}

	return applyWallpaperToWorkspace(ws, wallpaperPath)
}

func RenderWorkspaceWallpaper(sourcePath, outputPath, label string) error {
	label = NormalizeWorkspaceLabel(label)
	if label == "" {
		return fmt.Errorf("workspace label cannot be empty")
	}

	source, err := decodeWorkspaceWallpaper(sourcePath)
	if err != nil {
		return err
	}

	bounds := source.Bounds()
	canvas := image.NewRGBA(bounds)
	draw.Draw(canvas, bounds, source, bounds.Min, draw.Src)

	if err := drawWorkspaceLabelCorners(canvas, label); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(outputPath), "workspace-wallpaper-*.jpg")
	if err != nil {
		return err
	}

	encodeErr := jpeg.Encode(tmp, canvas, &jpeg.Options{Quality: workspaceWallpaperJPEGQuality})
	closeErr := tmp.Close()
	if encodeErr != nil {
		os.Remove(tmp.Name())
		return encodeErr
	}
	if closeErr != nil {
		os.Remove(tmp.Name())
		return closeErr
	}
	if err := os.Rename(tmp.Name(), outputPath); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func decodeWorkspaceWallpaper(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open source wallpaper: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode source wallpaper: %w", err)
	}
	return img, nil
}

func workspaceWallpaperSourceDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, defaultWorkspaceWallpaperSubdir)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("workspace wallpaper source dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace wallpaper source path %q is not a directory", dir)
	}
	return dir, nil
}

func workspaceWallpaperOutputPath(ws int) string {
	return filepath.Join(config.Dir(), workspaceWallpaperOutputDirName, fmt.Sprintf("workspace-%d.jpg", ws))
}

func listWorkspaceWallpaperCandidates(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read wallpaper directory: %w", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isSupportedWallpaperFile(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no supported wallpapers found in %s", dir)
	}
	return paths, nil
}

func isSupportedWallpaperFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png":
		return true
	default:
		return false
	}
}

func chooseRandomWallpaper(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no wallpaper candidates available")
	}
	if len(paths) == 1 {
		return paths[0], nil
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(paths))))
	if err != nil {
		return "", fmt.Errorf("choose wallpaper: %w", err)
	}
	return paths[n.Int64()], nil
}

func applyWallpaperToWorkspace(target int, wallpaperPath string) error {
	current, total, err := GetWorkspaceInfo()
	if err != nil {
		return err
	}
	if target < 1 || target > total {
		return fmt.Errorf("workspace %d does not exist on the current display (1-%d)", target, total)
	}

	switched := current != target
	if switched {
		if err := SwitchToWorkspace(target); err != nil {
			return err
		}
		time.Sleep(workspaceWallpaperSwitchDelay)
	}

	setErr := setWallpaperForCurrentDesktops(wallpaperPath)
	if setErr == nil {
		time.Sleep(workspaceWallpaperApplyDelay)
	}

	if switched {
		if err := SwitchToWorkspace(current); err != nil {
			if setErr != nil {
				return fmt.Errorf("%w; restore workspace %d: %v", setErr, current, err)
			}
			return fmt.Errorf("restore workspace %d: %w", current, err)
		}
		time.Sleep(workspaceWallpaperSwitchDelay)
	}

	return setErr
}

func setWallpaperForCurrentDesktops(path string) error {
	script := fmt.Sprintf(`
tell application "System Events"
	repeat with desktopRef in desktops
		set picture of desktopRef to POSIX file %q
	end repeat
end tell
`, path)
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("set desktop wallpaper: %w", err)
	}
	return nil
}

func drawWorkspaceLabelCorners(img *image.RGBA, label string) error {
	face, fittedLabel, err := bestWorkspaceLabelFace(label, img.Bounds())
	if err != nil {
		return err
	}

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	lineHeight := (metrics.Ascent + metrics.Descent).Ceil()
	textWidth := font.MeasureString(face, fittedLabel).Round()
	shortSide := minInt(img.Bounds().Dx(), img.Bounds().Dy())

	margin := maxInt(int(math.Round(float64(shortSide)*0.03)), 32)
	paddingX := maxInt(int(math.Round(float64(lineHeight)*0.55)), 18)
	paddingY := maxInt(int(math.Round(float64(lineHeight)*0.36)), 12)
	boxWidth := textWidth + paddingX*2
	boxHeight := lineHeight + paddingY*2

	corners := []image.Rectangle{
		image.Rect(img.Bounds().Min.X+margin, img.Bounds().Min.Y+margin, img.Bounds().Min.X+margin+boxWidth, img.Bounds().Min.Y+margin+boxHeight),
		image.Rect(img.Bounds().Max.X-margin-boxWidth, img.Bounds().Min.Y+margin, img.Bounds().Max.X-margin, img.Bounds().Min.Y+margin+boxHeight),
		image.Rect(img.Bounds().Min.X+margin, img.Bounds().Max.Y-margin-boxHeight, img.Bounds().Min.X+margin+boxWidth, img.Bounds().Max.Y-margin),
		image.Rect(img.Bounds().Max.X-margin-boxWidth, img.Bounds().Max.Y-margin-boxHeight, img.Bounds().Max.X-margin, img.Bounds().Max.Y-margin),
	}

	for _, box := range corners {
		draw.Draw(img, box, image.NewUniform(color.RGBA{0, 0, 0, workspaceWallpaperBoxAlpha}), image.Point{}, draw.Over)

		baseline := fixed.Point26_6{
			X: fixed.I(box.Min.X + paddingX),
			Y: fixed.I(box.Min.Y + paddingY + ascent),
		}

		drawer := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.RGBA{0, 0, 0, workspaceWallpaperTextShadowAlpha}),
			Face: face,
			Dot: fixed.Point26_6{
				X: baseline.X + fixed.I(2),
				Y: baseline.Y + fixed.I(2),
			},
		}
		drawer.DrawString(fittedLabel)

		drawer.Src = image.NewUniform(color.RGBA{255, 255, 255, workspaceWallpaperTextAlpha})
		drawer.Dot = baseline
		drawer.DrawString(fittedLabel)
	}

	return nil
}

func bestWorkspaceLabelFace(label string, bounds image.Rectangle) (font.Face, string, error) {
	baseFont, err := workspaceLabelFont()
	if err != nil {
		return nil, "", err
	}

	shortSide := minInt(bounds.Dx(), bounds.Dy())
	maxTextWidth := int(math.Round(float64(bounds.Dx()) * workspaceWallpaperWidthRatio))
	size := clampFloat(float64(shortSide)*workspaceWallpaperFontSizeRatio, minWorkspaceWallpaperFontSize, maxWorkspaceWallpaperFontSize)

	for size >= minWorkspaceWallpaperFontSize {
		face, err := opentype.NewFace(baseFont, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			return nil, "", err
		}
		if font.MeasureString(face, label).Round() <= maxTextWidth {
			return face, label, nil
		}
		size -= 4
	}

	face, err := opentype.NewFace(baseFont, &opentype.FaceOptions{
		Size:    minWorkspaceWallpaperFontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, "", err
	}
	return face, ellipsizeWorkspaceLabel(face, label, maxTextWidth), nil
}

func workspaceLabelFont() (*opentype.Font, error) {
	workspaceWallpaperFontOnce.Do(func() {
		workspaceWallpaperFont, workspaceWallpaperFontErr = opentype.Parse(gobold.TTF)
	})
	return workspaceWallpaperFont, workspaceWallpaperFontErr
}

func ellipsizeWorkspaceLabel(face font.Face, label string, maxWidth int) string {
	if font.MeasureString(face, label).Round() <= maxWidth {
		return label
	}

	runes := []rune(label)
	if len(runes) == 0 {
		return label
	}

	const ellipsis = "..."
	for len(runes) > 0 {
		candidate := string(runes) + ellipsis
		if font.MeasureString(face, candidate).Round() <= maxWidth {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return ellipsis
}

func clampFloat(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
