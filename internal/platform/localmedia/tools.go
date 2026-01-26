package localmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

// Tools is the “hard way” glue around system binaries.
//
// REQUIRED BINARIES in worker runtime:
// - libreoffice (soffice) for DOCX/PPTX -> PDF
// - pdftoppm (poppler-utils) for PDF -> page images
// - ffmpeg for video -> audio and keyframes
//
// This service is synchronous and deterministic, but should be called from worker jobs,
// not request handlers.
type Tools interface {
	AssertReady(ctx context.Context) error

	ConvertOfficeToPDF(ctx context.Context, inputPath string, outDir string) (pdfPath string, err error)
	CountPDFPages(ctx context.Context, pdfPath string) (int, error)
	RenderPDFToImages(ctx context.Context, pdfPath string, outDir string, opts PDFRenderOptions) ([]string, error)
	RenderPDFPage(ctx context.Context, pdfPath string, outDir string, page int, opts PDFRenderOptions) (string, error)

	ExtractAudioFromVideo(ctx context.Context, videoPath string, outPath string, opts AudioExtractOptions) (string, error)
	ExtractKeyframes(ctx context.Context, videoPath string, outDir string, opts KeyframeOptions) ([]string, error)

	// Helpers for callers who only have bytes:
	WriteTempFile(ctx context.Context, data []byte, suffix string) (string, func(), error)
}

// ---- Back-compat aliases so you don’t have to refactor call sites immediately ----
type MediaToolsService = Tools

func NewMediaToolsService(log *logger.Logger) MediaToolsService { return New(log) }

// -------------------------------------------------------------------------------

type PDFRenderOptions struct {
	DPI       int
	Format    string // "png" or "jpeg"
	FirstPage int    // 1-based, 0 means default
	LastPage  int    // 1-based, 0 means default
}

type AudioExtractOptions struct {
	SampleRateHz int
	Channels     int
	Format       string // "wav" or "flac"
}

type KeyframeOptions struct {
	IntervalSeconds float64
	SceneThreshold  float64

	Width       int
	MaxFrames   int
	Format      string // "jpg" or "png"
	JPEGQuality int
}

type tools struct {
	log *logger.Logger

	sofficePath  string
	pdftoppmPath string
	pdfinfoPath  string
	ffmpegPath   string

	workRoot string

	defaultTimeout time.Duration
}

func New(log *logger.Logger) Tools {
	slog := log.With("service", "MediaTools")
	return &tools{
		log:            slog,
		sofficePath:    "soffice",
		pdftoppmPath:   "pdftoppm",
		pdfinfoPath:    "pdfinfo",
		ffmpegPath:     "ffmpeg",
		workRoot:       "/tmp/neurobridge-media",
		defaultTimeout: 10 * time.Minute,
	}
}

func (m *tools) AssertReady(ctx context.Context) error {
	ctx = ctxutil.Default(ctx)
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

func (m *tools) assertBinary(ctx context.Context, name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("missing required binary %q in PATH: %w", name, err)
	}
	return nil
}

func (m *tools) WriteTempFile(ctx context.Context, data []byte, suffix string) (string, func(), error) {
	ctx = ctxutil.Default(ctx)
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

func (m *tools) ConvertOfficeToPDF(ctx context.Context, inputPath string, outDir string) (string, error) {
	ctx = ctxutil.Default(ctx)
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

	ctx, cancel := context.WithTimeout(ctx, m.defaultTimeout)
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

	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	pdfPath := filepath.Join(outDir, base+".pdf")

	if _, statErr := os.Stat(pdfPath); statErr != nil {
		pdfPath2, err2 := newestFileWithExt(outDir, ".pdf")
		if err2 != nil {
			return "", fmt.Errorf("pdf output not found at %s and scan failed: %v; soffice out=%s", pdfPath, err2, string(out))
		}
		pdfPath = pdfPath2
	}

	return pdfPath, nil
}

func (m *tools) CountPDFPages(ctx context.Context, pdfPath string) (int, error) {
	ctx = ctxutil.Default(ctx)
	if pdfPath == "" {
		return 0, fmt.Errorf("pdfPath required")
	}
	if _, err := exec.LookPath(m.pdfinfoPath); err != nil {
		return 0, fmt.Errorf("pdfinfo not found in PATH: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, m.pdfinfoPath, pdfPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo failed: %w; out=%s", err, string(out))
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Pages:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil || n <= 0 {
			continue
		}
		return n, nil
	}

	return 0, fmt.Errorf("pdfinfo output missing Pages field")
}

func (m *tools) RenderPDFToImages(ctx context.Context, pdfPath string, outDir string, opts PDFRenderOptions) ([]string, error) {
	ctx = ctxutil.Default(ctx)
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

	ctx, cancel := context.WithTimeout(ctx, m.defaultTimeout)
	defer cancel()

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

	paths, err := globSorted(outDir, "^page-\\d+\\.(png|jpe?g)$")
	if err != nil || len(paths) == 0 {
		paths2, _ := globSorted(outDir, ".*\\.(png|jpe?g)$")
		if len(paths2) == 0 {
			return nil, fmt.Errorf("no images produced by pdftoppm; out=%s", string(out))
		}
		return paths2, nil
	}
	return paths, nil
}

func (m *tools) RenderPDFPage(ctx context.Context, pdfPath string, outDir string, page int, opts PDFRenderOptions) (string, error) {
	ctx = ctxutil.Default(ctx)
	if err := m.AssertReady(ctx); err != nil {
		return "", err
	}
	if pdfPath == "" {
		return "", fmt.Errorf("pdfPath required")
	}
	if outDir == "" {
		return "", fmt.Errorf("outDir required")
	}
	if page <= 0 {
		return "", fmt.Errorf("page must be >= 1")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir outDir: %w", err)
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
		return "", fmt.Errorf("unsupported render format: %s", format)
	}

	ctx, cancel := context.WithTimeout(ctx, m.defaultTimeout)
	defer cancel()

	prefix := filepath.Join(outDir, fmt.Sprintf("page_%04d", page))
	args := []string{"-r", strconv.Itoa(dpi)}
	if format == "png" {
		args = append(args, "-png")
	} else {
		args = append(args, "-jpeg")
	}
	args = append(args, "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), pdfPath, prefix)

	cmd := exec.CommandContext(ctx, m.pdftoppmPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pdftoppm failed: %w; out=%s", err, string(out))
	}

	pattern := fmt.Sprintf("^page_%04d-\\d+\\.(png|jpe?g)$", page)
	paths, err := globSorted(outDir, pattern)
	if err != nil || len(paths) == 0 {
		paths2, _ := globSorted(outDir, ".*\\.(png|jpe?g)$")
		if len(paths2) == 0 {
			return "", fmt.Errorf("no images produced by pdftoppm; out=%s", string(out))
		}
		return paths2[0], nil
	}
	return paths[0], nil
}

func (m *tools) ExtractAudioFromVideo(ctx context.Context, videoPath string, outPath string, opts AudioExtractOptions) (string, error) {
	ctx = ctxutil.Default(ctx)
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

	ctx, cancel := context.WithTimeout(ctx, m.defaultTimeout)
	defer cancel()

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

func (m *tools) ExtractKeyframes(ctx context.Context, videoPath string, outDir string, opts KeyframeOptions) ([]string, error) {
	ctx = ctxutil.Default(ctx)
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
		maxFrames = 300
	}

	ctx, cancel := context.WithTimeout(ctx, m.defaultTimeout)
	defer cancel()

	outPattern := filepath.Join(outDir, "frame_%06d."+format)
	args := []string{"-y", "-i", videoPath}

	scaleFilter := ""
	if opts.Width > 0 {
		scaleFilter = fmt.Sprintf("scale=%d:-1", opts.Width)
	}

	var vf string
	if opts.SceneThreshold > 0 {
		vf = fmt.Sprintf("select='gt(scene\\,%0.3f)'", opts.SceneThreshold)
		if scaleFilter != "" {
			vf = vf + "," + scaleFilter
		}
	} else {
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
