package architecture_test

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestImportBoundaries(t *testing.T) {
	t.Helper()

	start, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := findModuleRoot(start)
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}

	modulePath, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read module path: %v", err)
	}

	internalDir := filepath.Join(root, "internal")
	fset := token.NewFileSet()

	type violation struct {
		file string
		imp  string
		rule string
	}
	var violations []violation

	walkErr := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", ".gocache":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		layer := layerFor(rel)
		if layer == "" {
			return nil
		}
		disallowed := disallowedImports(modulePath, layer)
		if len(disallowed) == 0 {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			if spec == nil || spec.Path == nil {
				continue
			}
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			for _, bad := range disallowed {
				if strings.HasPrefix(imp, bad) {
					violations = append(violations, violation{file: rel, imp: imp, rule: bad})
					break
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk internal/: %v", walkErr)
	}

	if len(violations) > 0 {
		var b strings.Builder
		b.WriteString("import boundary violations:\n")
		for _, v := range violations {
			fmt.Fprintf(&b, "- %s imports %q (disallowed: %q)\n", v.file, v.imp, v.rule)
		}
		t.Fatal(b.String())
	}
}

func TestNoClientsImportsOutsideClients(t *testing.T) {
	t.Helper()

	start, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := findModuleRoot(start)
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}

	modulePath, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read module path: %v", err)
	}

	internalDir := filepath.Join(root, "internal")
	fset := token.NewFileSet()

	type violation struct {
		file string
		imp  string
	}
	var violations []violation

	walkErr := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Allow shims to exist but ban importing them from elsewhere.
			if filepath.Base(path) == "clients" && filepath.Dir(path) == internalDir {
				return filepath.SkipDir
			}
			switch d.Name() {
			case ".git", "vendor", "node_modules", ".gocache":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			if spec == nil || spec.Path == nil {
				continue
			}
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if strings.HasPrefix(imp, modulePath+"/internal/clients/") {
				violations = append(violations, violation{file: rel, imp: imp})
				break
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk internal/: %v", walkErr)
	}

	if len(violations) > 0 {
		var b strings.Builder
		b.WriteString("internal/clients imports found outside internal/clients (use internal/platform instead):\n")
		for _, v := range violations {
			fmt.Fprintf(&b, "- %s imports %q\n", v.file, v.imp)
		}
		t.Fatal(b.String())
	}
}

func layerFor(rel string) string {
	switch {
	case strings.HasPrefix(rel, "internal/platform/"):
		return "platform"
	case strings.HasPrefix(rel, "internal/modules/"):
		return "modules"
	case strings.HasPrefix(rel, "internal/jobs/"):
		return "jobs"
	default:
		return ""
	}
}

func disallowedImports(modulePath string, layer string) []string {
	switch layer {
	case "platform":
		return []string{
			modulePath + "/internal/modules/",
			modulePath + "/internal/http/",
			modulePath + "/internal/jobs/",
		}
	case "modules":
		return []string{
			modulePath + "/internal/http/",
			modulePath + "/internal/jobs/",
		}
	case "jobs":
		return []string{
			modulePath + "/internal/http/",
		}
	default:
		return nil
	}
}

func findModuleRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", start)
		}
		dir = parent
	}
}

func readModulePath(goModPath string) (string, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		mp := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		if mp == "" {
			return "", fmt.Errorf("empty module path in %s", goModPath)
		}
		return mp, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("module path not found in %s", goModPath)
}
