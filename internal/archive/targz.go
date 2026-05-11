package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func PackDir(srcDir, dst string) (integrity string, size int64, err error) {
	return PackDirWithIgnore(srcDir, dst, nil)
}

func PackDirWithIgnore(srcDir, dst string, extra IgnoreMatcher) (integrity string, size int64, err error) {
	out, err := os.Create(dst)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()

	hasher := sha256.New()
	teeWriter := io.MultiWriter(out, hasher)

	gzw := gzip.NewWriter(teeWriter)
	tw := tar.NewWriter(gzw)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if extra != nil && extra(rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if walkErr != nil {
		return "", 0, walkErr
	}
	if err := tw.Close(); err != nil {
		return "", 0, err
	}
	if err := gzw.Close(); err != nil {
		return "", 0, err
	}
	if err := out.Close(); err != nil {
		return "", 0, err
	}
	st, err := os.Stat(dst)
	if err != nil {
		return "", 0, err
	}
	return "sha256-" + hex.EncodeToString(hasher.Sum(nil)), st.Size(), nil
}

func UnpackTo(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe tar entry: %s", hdr.Name)
		}
		target := filepath.Join(dst, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		default:
		}
	}
}

func shouldSkip(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts {
		switch p {
		case "build", "deps", "node_modules", ".git", "_storage", "_tmp":
			return true
		}
	}
	base := filepath.Base(rel)
	if strings.HasSuffix(base, ".tar.gz") {
		return true
	}
	if base == "bnl.lock" {
		return true
	}
	if strings.HasPrefix(base, ".env") {
		return true
	}
	return false
}
