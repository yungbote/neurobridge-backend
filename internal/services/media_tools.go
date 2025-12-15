package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/logger"
)

// MediaToolsService is the “hard way” glue around system binaries:
//
// REQUIRED BINARIES in worker runtime:
// - libreoffice (soffice) for DOCX/PPTX -> PDF
// - pdftoppm (poppler-utils) for PDF -> page images
// - ffmpeg for video -> audio and keyframes
//
// This service is synchronous and deterministic, but should be called from worker jobs,
// not request handlers.
type MediaToolsService interface {
	AssertReady(ctx context.Context) error

	ConvertOfficeToPDF(ctx context.Context, inputPath string, outDir string) (pdfPath string, err error)
	RenderPDFToImages(ctx context.Context, pdfPath string, outDir string, opts PDFRenderOptions) ([]string, error)

	ExtractAudioFromVideo(ctx context.Context, videoPath string, outPath string, opts AudioExtractOptions) (string, error)
	ExtractKeyframes(ctx context.Context, videoPath string, outDir string, opts KeyframeOptions) ([]string, error)

	// Helpers for callers who only have bytes:
	WriteTempFile(ctx context.Context, data []byte, suffix string) (string, func(), error)
}

type PDFRenderOptions struct {
	DPI       int    // e.g., 200–300
	Format    string // "png" or "jpeg"
	FirstPage int    // 1-based, 0 means default
	LastPage  int    // 1-based, 0 means default
}

type AudioExtractOptions struct {
	SampleRateHz int    // e.g., 16000
	Channels     int    // 1
	Format       string // "wav" or "flac"
}

type KeyframeOptions struct {
	// Choose ONE mode:
	IntervalSeconds float64 // e.g., 2.0 for every 2 seconds; 0 disables
	SceneThreshold  float64 // e.g., 0.35 for scene-change selection; 0 disables

	// Output
	Width           int    // 0 keep original; else scale width and keep aspect
	MaxFrames       int    // safety cap
	Format          string // "jpg" or "png"
	JPEGQuality     int    // 2..31 (lower is higher quality) for ffmpeg -q:v
}

type mediaToolsService struct {
	log *logger.Logger

	sofficePath  string
	pdftoppmPath string
	ffmpegPath   string

	workRoot string

	// hard caps
	defaultTimeout time.Duration
}

func NewMediaToolsService(log *logger.Logger) MediaToolsService {
	slog := log.With("service", "MediaToolsService")
	return &mediaToolsService{
		log:            slog,
		sofficePath:     "soffice",
		pdftoppmPath:    "pdftoppm",
		ffmpegPath:      "ffmpeg",
		workRoot:        "/tmp/neurobridge-media",
		defaultTimeout:  10 * time.Minute,
	}
}

func (m *mediaToolsService) AssertReady(ctx context.Context) error {
	ctx = defaultCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for _, bin := range []string{m.sofficePath, m.pdftoppmPath, m.ffmpegPath} {
		if err := m.assertBinary(ctx, bin); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(m.workRoot, 0o755); err != nil {
		return fmt.Errorf("create workRoot: %w", err)
	}
	return nil
}

func (m *mediaToolsService) assertBinary(ctx context.Context, name string) error {
	// Try `which` via exec.LookPath for portability
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("missing required binary %q in PATH: %w", name, err)
	}
	// Optionally run `name -version` quickly for sanity
	return nil
}

func (m *mediaToolsService) WriteTempFile(ctx context.Context, data []byte, suffix string) (string, func(), error) {
	ctx = defaultCtx(ctx)
	if err := os.MkdirAll(m.workRoot, 0o755); err != nil {
		return "", func() {}, fmt.Errorf("mkdir workRoot: %w", err)
	}
	h := sha256.Sum256(data)
	base := hex.EncodeToString(h[:])[:16]
	if suffix != "" && !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	path := filepath.Join(m.workRoot, fmt.Sprintf("%s%s", base, suffix))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", func() {}, fmt.Errorf("write temp file: %w", err)
	}
	cleanup := func() { _ = os.Remove(path) }
	return path, cleanup, nil
}

func (m *mediaToolsService) ConvertOfficeToPDF(ctx context.Context, inputPath string, outDir string) (string, error) {
	ctx = defaultCtx(ctx)
	if err := m.AssertReady(ctx); err != nil {
		return "", err
	}
	if inputPath == "" {
		return "", fmt.Errorf("inputPath required")
	}
	if outDir == "" {
		return "", fmt.Errorf("outDir required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir outDir: %w", err)
	}

	// LibreOffice headless conversion (deterministic)
	// Output PDF is named based on input filename.
	timeout := m.defaultTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.sofficePath,
		"--headless",
		"--nologo",
		"--nolockcheck",
		"--nodefault",
		"--norestore",
		"--convert-to", "pdf",
		"--outdir", outDir,
		inputPath,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("soffice convert failed: %w; out=%s", err, string(out))
	}

	// Determine output path
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	pdfPath := filepath.Join(outDir, base+".pdf")

	// LibreOffice sometimes changes casing or sanitizes names; fallback: scan outDir for newest PDF
	if _, statErr := os.Stat(pdfPath); statErr != nil {
		pdfPath2, err2 := newestFileWithExt(outDir, ".pdf")
		if err2 != nil {
			return "", fmt.Errorf("pdf output not found at %s and scan failed: %v; soffice out=%s", pdfPath, err2, string(out))
		}
		pdfPath = pdfPath2
	}

	return pdfPath, nil
}

func (m *mediaToolsService) RenderPDFToImages(ctx context.Context, pdfPath string, outDir string, opts PDFRenderOptions) ([]string, error) {
	ctx = defaultCtx(ctx)
	if err := m.AssertReady(ctx); err != nil {
		return nil, err
	}
	if pdfPath == "" {
		return nil, fmt.Errorf("pdfPath required")
	}
	if outDir == "" {
		return nil, fmt.Errorf("outDir required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir outDir: %w", err)
	}

	dpi := opts.DPI
	if dpi <= 0 {
		dpi = 200
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "png"
	}
	if format != "png" && format != "jpeg" && format != "jpg" {
		return nil, fmt.Errorf("unsupported render format: %s", format)
	}

	timeout := m.defaultTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// pdftoppm usage:
	// pdftoppm -r 200 -png -f 1 -l 3 input.pdf outprefix
	prefix := filepath.Join(outDir, "page")
	args := []string{"-r", strconv.Itoa(dpi)}
	if format == "png" {
		args = append(args, "-png")
	} else {
		args = append(args, "-jpeg")
	}
	if opts.FirstPage > 0 {
		args = append(args, "-f", strconv.Itoa(opts.FirstPage))
	}
	if opts.LastPage > 0 {
		args = append(args, "-l", strconv.Itoa(opts.LastPage))
	}
	args = append(args, pdfPath, prefix)

	cmd := exec.CommandContext(ctx, m.pdftoppmPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pdftoppm failed: %w; out=%s", err, string(out))
	}

	// Collect generated images:
	// page-1.png, page-2.png ... or page-01.jpg depending on tool/version.
	paths, err := globSorted(outDir, "^page-\\d+\\.(png|jpe?g)$")
	if err != nil || len(paths) == 0 {
		// fallback: scan any images
		paths2, _ := globSorted(outDir, ".*\\.(png|jpe?g)$")
		if len(paths2) == 0 {
			return nil, fmt.Errorf("no images produced by pdftoppm; out=%s", string(out))
		}
		return paths2, nil
	}
	return paths, nil
}

func (m *mediaToolsService) ExtractAudioFromVideo(ctx context.Context, videoPath string, outPath string, opts AudioExtractOptions) (string, error) {
	ctx = defaultCtx(ctx)
	if err := m.AssertReady(ctx); err != nil {
		return "", err
	}
	if videoPath == "" {
		return "", fmt.Errorf("videoPath required")
	}
	if outPath == "" {
		return "", fmt.Errorf("outPath required")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir outPath dir: %w", err)
	}

	sr := opts.SampleRateHz
	if sr <= 0 {
		sr = 16000
	}
	ch := opts.Channels
	if ch <= 0 {
		ch = 1
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "wav"
	}
	if format != "wav" && format != "flac" {
		return "", fmt.Errorf("unsupported audio format: %s", format)
	}

	timeout := m.defaultTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// ffmpeg -i in.mp4 -vn -ac 1 -ar 16000 -f wav out.wav
	args := []string{
		"-y",
		"-i", videoPath,
		"-vn",
		"-ac", strconv.Itoa(ch),
		"-ar", strconv.Itoa(sr),
	}
	if format == "wav" {
		args = append(args, "-f", "wav", outPath)
	} else {
		args = append(args, "-f", "flac", outPath)
	}

	cmd := exec.CommandContext(ctx, m.ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg extract audio failed: %w; out=%s", err, string(out))
	}

	if _, err := os.Stat(outPath); err != nil {
		return "", fmt.Errorf("audio output missing at %s", outPath)
	}
	return outPath, nil
}

func (m *mediaToolsService) ExtractKeyframes(ctx context.Context, videoPath string, outDir string, opts KeyframeOptions) ([]string, error) {
	ctx = defaultCtx(ctx)
	if err := m.AssertReady(ctx); err != nil {
		return nil, err
	}
	if videoPath == "" {
		return nil, fmt.Errorf("videoPath required")
	}
	if outDir == "" {
		return nil, fmt.Errorf("outDir required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir outDir: %w", err)
	}

	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "jpg"
	}
	if format != "jpg" && format != "jpeg" && format != "png" {
		return nil, fmt.Errorf("unsupported keyframe format: %s", format)
	}

	maxFrames := opts.MaxFrames
	if maxFrames <= 0 {
		maxFrames = 300 // hard cap for safety
	}

	timeout := m.defaultTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outPattern := filepath.Join(outDir, "frame_%06d."+format)

	args := []string{"-y", "-i", videoPath}

	// Scale if requested
	scaleFilter := ""
	if opts.Width > 0 {
		// keep aspect: scale=WIDTH:-1
		scaleFilter = fmt.Sprintf("scale=%d:-1", opts.Width)
	}

	// Choose selection method
	var vf string
	if opts.SceneThreshold > 0 {
		// scene detect selection:
		// select='gt(scene,0.35)'
		vf = fmt.Sprintf("select='gt(scene\\,%0.3f)'", opts.SceneThreshold)
		if scaleFilter != "" {
			vf = vf + "," + scaleFilter
		}
	} else {
		// interval extraction via fps
		interval := opts.IntervalSeconds
		if interval <= 0 {
			interval = 2.0
		}
		fps := 1.0 / interval
		vf = fmt.Sprintf("fps=%0.6f", fps)
		if scaleFilter != "" {
			vf = vf + "," + scaleFilter
		}
	}

	args = append(args, "-vf", vf)

	// Quality
	if format == "jpg" || format == "jpeg" {
		q := opts.JPEGQuality
		if q <= 0 {
			q = 3
		}
		args = append(args, "-q:v", strconv.Itoa(q))
	}

	args = append(args, outPattern)

	cmd := exec.CommandContext(ctx, m.ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg keyframes failed: %w; out=%s", err, string(out))
	}

	frames, _ := globSorted(outDir, "^frame_\\d+\\.(png|jpe?g)$")
	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames produced by ffmpeg; out=%s", string(out))
	}
	if len(frames) > maxFrames {
		frames = frames[:maxFrames]
	}

	return frames, nil
}

// ---------- helpers ----------

func newestFileWithExt(dir, ext string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(e.Name())) != ext {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest = filepath.Join(dir, e.Name())
			newestMod = info.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no %s files in %s", ext, dir)
	}
	return newest, nil
}

func globSorted(dir string, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if re.MatchString(strings.ToLower(e.Name())) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func defaultCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}










