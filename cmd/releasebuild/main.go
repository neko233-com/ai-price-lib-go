// releasebuild builds distributable CLI archives with correct Unix executable modes.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type target struct{ os, arch string }
type item struct {
	path, name string
	mode       int64
}

func fail(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func main() {
	out := flag.String("out", "", "new output directory")
	goCmd := flag.String("go", "go", "Go executable")
	flag.Parse()
	if *out == "" {
		fail(fmt.Errorf("-out is required"))
	}
	fail(os.Mkdir(*out, 0755))
	ts := []target{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}
	var sums []string
	for _, t := range ts {
		bin := "aiprice"
		if t.os == "windows" {
			bin += ".exe"
		}
		tmp := filepath.Join(*out, ".stage-"+t.os+"-"+t.arch)
		cmd := exec.Command(*goCmd, "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", tmp, "./cmd/aiprice")
		cmd.Env = append(os.Environ(), "GOOS="+t.os, "GOARCH="+t.arch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fail(cmd.Run())
		items := []item{{tmp, bin, 0755}, {"README.md", "README.md", 0644}, {"README_zh-CN.md", "README_zh-CN.md", 0644}, {"LICENSE", "LICENSE", 0644}, {"NOTICE", "NOTICE", 0644}, {"THIRD_PARTY_MODELS_DEV_LICENSE", "THIRD_PARTY_MODELS_DEV_LICENSE", 0644}, {"licenses/gofrs-flock.txt", "licenses/gofrs-flock.txt", 0644}, {"licenses/golang-x-sys.txt", "licenses/golang-x-sys.txt", 0644}, {"examples/aiprice.json", "examples/aiprice.json", 0644}, {"examples/media.json", "examples/media.json", 0644}}
		name := "aiprice_" + t.os + "_" + t.arch
		var err error
		if t.os == "windows" {
			name += ".zip"
			err = writeZip(filepath.Join(*out, name), items)
		} else {
			name += ".tar.gz"
			err = writeTar(filepath.Join(*out, name), items)
		}
		fail(err)
		fail(os.Remove(tmp))
		data, err := os.ReadFile(filepath.Join(*out, name))
		fail(err)
		sums = append(sums, fmt.Sprintf("%x  %s", sha256.Sum256(data), name))
		fmt.Println(name)
	}
	fail(os.WriteFile(filepath.Join(*out, "SHA256SUMS.txt"), []byte(strings.Join(sums, "\n")+"\n"), 0644))
}
func writeZip(path string, items []item) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	z := zip.NewWriter(f)
	for _, it := range items {
		data, e := os.ReadFile(it.path)
		if e != nil {
			_ = z.Close()
			_ = f.Close()
			return e
		}
		h := &zip.FileHeader{Name: it.name, Method: zip.Deflate}
		h.SetMode(os.FileMode(it.mode))
		h.SetModTime(time.Unix(0, 0))
		w, e := z.CreateHeader(h)
		if e == nil {
			_, e = w.Write(data)
		}
		if e != nil {
			_ = z.Close()
			_ = f.Close()
			return e
		}
	}
	if e = z.Close(); e != nil {
		_ = f.Close()
		return e
	}
	return f.Close()
}
func writeTar(path string, items []item) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	g := gzip.NewWriter(f)
	w := tar.NewWriter(g)
	for _, it := range items {
		data, e := os.ReadFile(it.path)
		if e != nil {
			_ = w.Close()
			_ = g.Close()
			_ = f.Close()
			return e
		}
		e = w.WriteHeader(&tar.Header{Name: it.name, Mode: it.mode, Size: int64(len(data)), ModTime: time.Unix(0, 0)})
		if e == nil {
			_, e = io.Copy(w, bytes.NewReader(data))
		}
		if e != nil {
			_ = w.Close()
			_ = g.Close()
			_ = f.Close()
			return e
		}
	}
	if e = w.Close(); e != nil {
		_ = g.Close()
		_ = f.Close()
		return e
	}
	if e = g.Close(); e != nil {
		_ = f.Close()
		return e
	}
	return f.Close()
}
