package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.2.0", 0},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.3.0-rc.1", "1.3.0", -1},
		{"1.3.0", "1.3.0-rc.1", 1},
		{"1.3.0-rc.1", "1.3.0-rc.2", -1},
		{"1.3.0+build.5", "1.3.0", 0},
	}
	for _, c := range cases {
		got, err := Compare(c.a, c.b)
		if err != nil {
			t.Fatalf("Compare(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"dev", "1.2", "1.x.0", ""} {
		if _, err := Compare(bad, "1.0.0"); err == nil {
			t.Errorf("Compare(%q, ...) should fail", bad)
		}
	}
}

func TestArchiveName(t *testing.T) {
	if got := ArchiveName("linux", "amd64"); got != "howdy_linux_amd64.tar.gz" {
		t.Errorf("linux: %s", got)
	}
	if got := ArchiveName("windows", "arm64"); got != "howdy_windows_arm64.zip" {
		t.Errorf("windows: %s", got)
	}
}

// server serves one release, v1.2.0, with archives for linux/amd64 and
// windows/amd64 and their checksums.
func server(t *testing.T, binary []byte, corrupt bool) (*httptest.Server, Client) {
	t.Helper()
	files := map[string][]byte{
		ArchiveName("linux", "amd64"):   tarGz(t, "howdy", binary),
		ArchiveName("windows", "amd64"): zipped(t, "howdy.exe", binary),
	}
	var sums strings.Builder
	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	if corrupt {
		files[ArchiveName("linux", "amd64")] = tarGz(t, "howdy", []byte("tampered"))
	}
	files[checksums] = []byte(sums.String())

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	release := Release{Tag: "v1.2.0", URL: srv.URL + "/release"}
	for name := range files {
		release.Assets = append(release.Assets, Asset{Name: name, URL: srv.URL + "/download/" + name})
	}
	mux.HandleFunc("/repos/owner/howdy/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("the token was not sent to the API")
		}
		_ = json.NewEncoder(w).Encode(release)
	})
	mux.HandleFunc("/repos/owner/howdy/releases/tags/v1.2.0", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(release)
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(files[strings.TrimPrefix(r.URL.Path, "/download/")])
	})
	return srv, Client{HTTP: srv.Client(), API: srv.URL, Repo: "owner/howdy", Token: "secret"}
}

func TestLatestAndTagged(t *testing.T) {
	_, c := server(t, []byte("new howdy"), false)
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version() != "1.2.0" {
		t.Errorf("latest is %s", r.Version())
	}
	if _, err := c.Tagged(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	_, err = c.Tagged(context.Background(), "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "has no release v9.9.9") {
		t.Errorf("a missing release: %v", err)
	}
}

func TestDownload(t *testing.T) {
	_, c := server(t, []byte("new howdy"), false)
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, goos := range []string{"linux", "windows"} {
		got, err := c.download(context.Background(), r, goos, "amd64")
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if string(got) != "new howdy" {
			t.Errorf("%s: got %q", goos, got)
		}
	}
	if _, err := c.download(context.Background(), r, "plan9", "amd64"); err == nil || !strings.Contains(err.Error(), "no build for plan9/amd64") {
		t.Errorf("a platform with no build: %v", err)
	}
}

func TestDownloadRejectsABadChecksum(t *testing.T) {
	_, c := server(t, []byte("new howdy"), true)
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.download(context.Background(), r, "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Errorf("a tampered archive: %v", err)
	}
}

func TestReplace(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "howdy")
	if err := os.WriteFile(exe, []byte("old howdy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(exe, []byte("new howdy")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new howdy" {
		t.Errorf("got %q", got)
	}
	info, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the new howdy is not executable: %v", info.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("left files behind: %v", entries)
	}
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("readme")}, {name, data}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
