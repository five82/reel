package encoder

import (
	"encoding/binary"
	"fmt"
	"io"
)

// writeIVFHeader writes a 32-byte IVF file header.
func writeIVFHeader(w io.Writer, width, height uint16, fpsNum, fpsDen uint32) error {
	var hdr [32]byte
	copy(hdr[0:4], "DKIF")
	binary.LittleEndian.PutUint16(hdr[4:6], 0)  // version
	binary.LittleEndian.PutUint16(hdr[6:8], 32) // header size
	copy(hdr[8:12], "AV01")
	binary.LittleEndian.PutUint16(hdr[12:14], width)
	binary.LittleEndian.PutUint16(hdr[14:16], height)
	binary.LittleEndian.PutUint32(hdr[16:20], fpsNum)
	binary.LittleEndian.PutUint32(hdr[20:24], fpsDen)
	// bytes 24-28: number of frames (patched during merge, 0 here)
	// bytes 28-32: unused
	_, err := w.Write(hdr[:])
	if err != nil {
		return fmt.Errorf("failed to write IVF header: %w", err)
	}
	return nil
}

// writeIVFFrame writes a single IVF frame (4 bytes size + 8 bytes pts + data).
func writeIVFFrame(w io.Writer, data []byte, pts int64) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("failed to write IVF frame size: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint64(pts)); err != nil {
		return fmt.Errorf("failed to write IVF frame timestamp: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write IVF frame data: %w", err)
	}
	return nil
}
