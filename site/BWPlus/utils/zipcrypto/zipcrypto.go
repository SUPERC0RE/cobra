// Package zipcrypto creates password-protected ZIP archives using the
// traditional PKWARE ZipCrypto cipher, implemented on the Go standard
// library (crc32 + a 3-key state machine) so no third-party dependency
// is needed. ZipCrypto is deliberately chosen for compatibility: WinRAR,
// 7-Zip, Windows Explorer and macOS `unzip` all open it without extra
// tooling.
package zipcrypto

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"
)

type fileEntry struct {
	name string
	data []byte
}

// WriteArchive writes an encrypted archive to path. Each entry in files
// is stored (deflate-compressed, ZipCrypto-encrypted) under its name.
func WriteArchive(path, password string, files map[string][]byte) error {
	if len(files) == 0 {
		return fmt.Errorf("zipcrypto: no files to archive")
	}
	var entries []fileEntry
	for name, data := range files {
		entries = append(entries, fileEntry{name: name, data: data})
	}

	// Build the whole archive in memory so entry sizes are known before
	// writing (required for both local headers and central directory).
	var buf bytes.Buffer
	var central []byte
	var offset uint32

	for _, fe := range entries {
		compressed, crc, err := compressEntry(fe.data)
		if err != nil {
			return err
		}
		encrypted := encrypt(compressed, crc, password)
		if err := writeLocalHeader(&buf, fe.name, crc, encrypted, len(fe.data)); err != nil {
			return err
		}
		off := offset
		offset += uint32(buf.Len() - int(off))
		buf.Write(encrypted)
		offset += uint32(len(encrypted))

		cd := makeCentralHeader(fe.name, crc, encrypted, uint32(len(fe.data)), off)
		central = append(central, cd...)
	}

	// Central directory.
	cdStart := uint32(buf.Len())
	buf.Write(central)
	cdSize := uint32(len(central))

	// End of central directory.
	var eocd [22]byte
	binary.LittleEndian.PutUint32(eocd[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[4:], 0) // disk number
	binary.LittleEndian.PutUint16(eocd[6:], 0) // cd start disk
	binary.LittleEndian.PutUint16(eocd[8:], uint16(len(entries)))
	binary.LittleEndian.PutUint16(eocd[10:], uint16(len(entries)))
	binary.LittleEndian.PutUint32(eocd[12:], cdSize)
	binary.LittleEndian.PutUint32(eocd[16:], cdStart)
	binary.LittleEndian.PutUint16(eocd[20:], 0) // comment length
	buf.Write(eocd[:])

	return os.WriteFile(path, buf.Bytes(), 0o600)
}

// compressEntry deflates data and returns the compressed bytes and the
// CRC-32 of the plaintext (used both for the encryption check byte and
// the archive's crc field).
func compressEntry(data []byte) ([]byte, uint32, error) {
	var out bytes.Buffer
	w, err := flate.NewWriter(&out, flate.BestCompression)
	if err != nil {
		return nil, 0, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, 0, err
	}
	if err := w.Close(); err != nil {
		return nil, 0, err
	}
	return out.Bytes(), crc32.ChecksumIEEE(data), nil
}

// writeLocalHeader writes the per-file local header. General-purpose bit
// 0 is set to mark the entry encrypted; method 8 = deflate.
func writeLocalHeader(w io.Writer, name string, crc uint32, encrypted []byte, uncompressed int) error {
	binaryDigest := func() []byte {
		h := make([]byte, 30+len(name))
		binary.LittleEndian.PutUint32(h[0:], 0x04034b50)
		binary.LittleEndian.PutUint16(h[4:], 20) // version needed
		binary.LittleEndian.PutUint16(h[6:], 1)  // flags: bit0 encrypted
		binary.LittleEndian.PutUint16(h[8:], 8)  // method: deflate
		binary.LittleEndian.PutUint16(h[10:], dosTime(time.Now()))
		binary.LittleEndian.PutUint16(h[12:], dosDate(time.Now()))
		binary.LittleEndian.PutUint32(h[14:], crc)
		binary.LittleEndian.PutUint32(h[18:], uint32(len(encrypted))) // compressed size (incl. 12B cipher header)
		binary.LittleEndian.PutUint32(h[22:], uint32(uncompressed))
		binary.LittleEndian.PutUint16(h[26:], uint16(len(name)))
		binary.LittleEndian.PutUint16(h[28:], 0) // extra length
		copy(h[30:], name)
		return h
	}
	_, err := w.Write(binaryDigest())
	return err
}

func makeCentralHeader(name string, crc uint32, encrypted []byte, uncompressed, localOffset uint32) []byte {
	h := make([]byte, 46+len(name))
	binary.LittleEndian.PutUint32(h[0:], 0x02014b50)
	binary.LittleEndian.PutUint16(h[4:], 20) // version made by
	binary.LittleEndian.PutUint16(h[6:], 20) // version needed
	binary.LittleEndian.PutUint16(h[8:], 1)  // flags: encrypted
	binary.LittleEndian.PutUint16(h[10:], 8) // method: deflate
	binary.LittleEndian.PutUint16(h[12:], dosTime(time.Now()))
	binary.LittleEndian.PutUint16(h[14:], dosDate(time.Now()))
	binary.LittleEndian.PutUint32(h[16:], crc)
	binary.LittleEndian.PutUint32(h[20:], uint32(len(encrypted)))
	binary.LittleEndian.PutUint32(h[24:], uint32(uncompressed))
	binary.LittleEndian.PutUint16(h[28:], uint16(len(name)))
	binary.LittleEndian.PutUint16(h[30:], 0) // extra
	binary.LittleEndian.PutUint16(h[32:], 0) // comment
	binary.LittleEndian.PutUint16(h[34:], 0) // disk number start
	binary.LittleEndian.PutUint16(h[36:], 0) // internal attrs
	binary.LittleEndian.PutUint32(h[38:], 0) // external attrs
	binary.LittleEndian.PutUint32(h[42:], localOffset)
	copy(h[46:], name)
	return h
}

// dosTime/dosDate convert a time.Time to the ZIP DOS time/date fields.
func dosTime(t time.Time) uint16 {
	return uint16(t.Hour()<<11 | t.Minute()<<5 | (t.Second() / 2))
}

func dosDate(t time.Time) uint16 {
	return uint16((t.Year()-1980)<<9 | int(t.Month())<<5 | t.Day())
}

// --- ZipCrypto cipher ---

// zipKeys is the 3-key PRNG state machine underlying PKWARE ZipCrypto.
type zipKeys struct {
	key0, key1, key2 uint32
}

// newZipKeys initialises the state machine and consumes the password.
func newZipKeys(password string) *zipKeys {
	k := &zipKeys{key0: 0x12345678, key1: 0x23456789, key2: 0x34567890}
	for i := 0; i < len(password); i++ {
		k.update(password[i])
	}
	return k
}

// update runs the key evolution on one plaintext byte, mirroring the
// InfoZIP update_keys() / CRC32() macro: a raw table lookup with no
// initial/final bit-flip (unlike Go's full crc32.Update).
func (k *zipKeys) update(c byte) {
	k.key0 = (k.key0 >> 8) ^ crc32.IEEETable[(k.key0^uint32(c))&0xff]
	k.key1 = (k.key1+(k.key0&0xff))*134775813 + 1
	k.key2 = (k.key2 >> 8) ^ crc32.IEEETable[(k.key2^(k.key1>>24))&0xff]
}

// decryptByte returns the keystream byte used to XOR with plaintext.
func (k *zipKeys) decryptByte() byte {
	temp := k.key2 | 2
	return byte((temp * (temp ^ 1)) >> 8)
}

// encrypt scrambles the deflated stream into the ciphertext. The first
// 12 bytes are the cipher header whose final byte carries (crc>>24) so
// password checks can succeed without the CRC field itself.
func encrypt(compressed []byte, crc uint32, password string) []byte {
	keys := newZipKeys(password)

	out := make([]byte, 12+len(compressed))

	// 12-byte encryption header: 11 pseudo-random bytes + CRC check byte.
	seed := uint32(crc)
	for i := 0; i < 12; i++ {
		seed = seed*1664525 + 1013904223
		b := byte(seed >> 24)
		if i == 11 {
			b = byte(crc >> 24) // CRC-32 high byte used for password verification
		}
		out[i] = keys.scramble(b)
	}

	for i, b := range compressed {
		out[12+i] = keys.scramble(b)
	}
	return out
}

// scramble outputs the cipher byte for one plaintext byte and advances
// the key state with the plaintext byte, per the ZipCrypto spec.
func (k *zipKeys) scramble(b byte) byte {
	c := b ^ k.decryptByte()
	k.update(b)
	return c
}
