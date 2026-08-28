package zipcrypto

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"io"
	"path/filepath"
	"testing"
)

// decrypt mirrors the ZipCrypto scheme (test-only) to verify encrypt()
// is self-consistent and produces the original deflate stream.
func decrypt(cipher []byte, password string) []byte {
	keys := newZipKeys(password)
	out := make([]byte, len(cipher)-12)
	for i := 0; i < 12; i++ {
		_ = keys.unscramble(cipher[i])
	}
	for i, b := range cipher[12:] {
		out[i] = keys.unscramble(b)
	}
	return out
}

func (k *zipKeys) unscramble(b byte) byte {
	c := b ^ k.decryptByte()
	k.update(c)
	return c
}

func TestRoundTrip(t *testing.T) {
	const password = "wt123321"
	files := map[string][]byte{
		"mach.pw": bytes.Repeat([]byte(`{"url":"https://a.com"}`), 100),
		"mach.ck": []byte(`[{"host":".a.com","name":"sid"}]`),
	}

	zipPath := filepath.Join(t.TempDir(), "out.zip")
	if err := WriteArchive(zipPath, password, files); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer r.Close()

	if len(r.File) != len(files) {
		t.Fatalf("got %d entries, want %d", len(r.File), len(files))
	}

	for _, f := range r.File {
		if f.Flags&1 == 0 {
			t.Errorf("entry %s: not marked encrypted", f.Name)
		}
		rc, err := f.OpenRaw()
		if err != nil {
			t.Fatalf("open raw %s: %v", f.Name, err)
		}
		cipher, _ := io.ReadAll(rc)
		t.Logf("entry %s: CompressedSize=%d Method=%d Flags=%#x read=%d",
			f.Name, f.CompressedSize64, f.Method, f.Flags, len(cipher))

		want := files[f.Name]
		if uint64(len(want)) != f.UncompressedSize64 {
			t.Errorf("entry %s: uncompressed size %d, want %d", f.Name, f.UncompressedSize64, len(want))
		}

		comp := decrypt(cipher, password)
		got := inflate(t, comp)
		if !bytes.Equal(got, want) {
			t.Errorf("entry %s: content mismatch, got %d bytes want %d", f.Name, len(got), len(want))
		}
		if crc32.ChecksumIEEE(got) != f.CRC32 {
			t.Errorf("entry %s: CRC mismatch", f.Name)
		}
	}
}

func inflate(t *testing.T, comp []byte) []byte {
	t.Helper()
	fr := flate.NewReader(bytes.NewReader(comp))
	defer fr.Close()
	got, err := io.ReadAll(fr)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}
	return got
}
