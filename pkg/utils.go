package utils

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	envelopeMagic     = "YTE1"
	envelopePrefixLen = 4 + 2 + 8 + sha256.Size
)

var (
	ErrIncompleteHeader = errors.New("incomplete envelope header")
	ErrInvalidHeader    = errors.New("invalid envelope header")
)

var Palette = [16][4]byte{
	{8, 8, 8, 255},
	{24, 24, 24, 255},
	{40, 40, 40, 255},
	{56, 56, 56, 255},
	{72, 72, 72, 255},
	{88, 88, 88, 255},
	{104, 104, 104, 255},
	{120, 120, 120, 255},
	{136, 136, 136, 255},
	{152, 152, 152, 255},
	{168, 168, 168, 255},
	{184, 184, 184, 255},
	{200, 200, 200, 255},
	{216, 216, 216, 255},
	{232, 232, 232, 255},
	{248, 248, 248, 255},
}

type VideoConfig struct {
	Width    int
	Height   int
	CellSize int
	FPS      int
}

type EnvelopeHeader struct {
	FileName string
	Size     uint64
	Hash     [sha256.Size]byte
}

func DefaultConfig() VideoConfig {
	return VideoConfig{
		Width:    1920,
		Height:   1080,
		CellSize: 8,
		FPS:      24,
	}
}

func (c VideoConfig) Validate() error {
	if c.Width <= 0 || c.Height <= 0 || c.CellSize <= 0 || c.FPS <= 0 {
		return fmt.Errorf("all video config values must be positive")
	}
	if c.Width%c.CellSize != 0 || c.Height%c.CellSize != 0 {
		return fmt.Errorf("width and height must be divisible by cell size")
	}
	if c.CellsPerFrame()%2 != 0 {
		return fmt.Errorf("cells per frame must be even")
	}
	return nil
}

func (c VideoConfig) BlocksX() int {
	return c.Width / c.CellSize
}

func (c VideoConfig) BlocksY() int {
	return c.Height / c.CellSize
}

func (c VideoConfig) CellsPerFrame() int {
	return c.BlocksX() * c.BlocksY()
}

func (c VideoConfig) BytesPerFrame() int {
	return c.CellsPerFrame() / 2
}

func (c VideoConfig) FrameSizeBytes() int {
	return c.Width * c.Height * 4
}

func WorkerCount(requested int) int {
	if requested > 0 {
		return requested
	}
	count := runtime.NumCPU()
	if count < 1 {
		return 1
	}
	return count
}

func ByteToNibbles(value byte) (uint8, uint8) {
	return value >> 4, value & 0x0F
}

func NibblesToByte(high, low uint8) byte {
	return (high << 4) | (low & 0x0F)
}

func ClosestPaletteIndex(r, g, b byte) uint8 {
	for index, clr := range Palette {
		if clr[0] == r && clr[1] == g && clr[2] == b {
			return uint8(index)
		}
	}

	bestIndex := uint8(0)
	bestDistance := int(^uint(0) >> 1)

	for index, clr := range Palette {
		dr := int(r) - int(clr[0])
		dg := int(g) - int(clr[1])
		db := int(b) - int(clr[2])
		distance := dr*dr + dg*dg + db*db
		if distance < bestDistance {
			bestDistance = distance
			bestIndex = uint8(index)
		}
	}

	return bestIndex
}

func BuildEnvelope(inputPath string, payload []byte) ([]byte, error) {
	fileName := filepath.Base(inputPath)
	if len(fileName) > 65535 {
		return nil, fmt.Errorf("file name is too long to embed")
	}

	sum := sha256.Sum256(payload)
	var header bytes.Buffer
	header.Grow(envelopePrefixLen + len(fileName) + len(payload))
	header.WriteString(envelopeMagic)

	var nameLen [2]byte
	binary.BigEndian.PutUint16(nameLen[:], uint16(len(fileName)))
	header.Write(nameLen[:])

	var payloadLen [8]byte
	binary.BigEndian.PutUint64(payloadLen[:], uint64(len(payload)))
	header.Write(payloadLen[:])

	header.Write(sum[:])
	header.WriteString(fileName)
	header.Write(payload)

	return header.Bytes(), nil
}

func ParseEnvelopeHeader(data []byte) (EnvelopeHeader, int, error) {
	if len(data) < envelopePrefixLen {
		return EnvelopeHeader{}, 0, ErrIncompleteHeader
	}
	if string(data[:4]) != envelopeMagic {
		return EnvelopeHeader{}, 0, ErrInvalidHeader
	}

	nameLen := int(binary.BigEndian.Uint16(data[4:6]))
	totalHeaderLen := envelopePrefixLen + nameLen
	if len(data) < totalHeaderLen {
		return EnvelopeHeader{}, 0, ErrIncompleteHeader
	}

	var header EnvelopeHeader
	header.Size = binary.BigEndian.Uint64(data[6:14])
	copy(header.Hash[:], data[14:46])
	header.FileName = string(data[46:totalHeaderLen])

	return header, totalHeaderLen, nil
}

func BuildEncodeArgs(config VideoConfig, outputPath string) ([]string, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-s:v", fmt.Sprintf("%dx%d", config.Width, config.Height),
		"-r", fmt.Sprintf("%d", config.FPS),
		"-i", "pipe:0",
		"-an",
		"-threads", "0",
		"-y",
	}

	ext := strings.ToLower(filepath.Ext(outputPath))
	switch ext {
	case ".mp4", ".mov":
		args = append(args,
			"-vf", "format=yuv420p",
			"-c:v", "libx264",
			"-crf", "1",
			"-preset", "veryfast",
			"-profile:v", "high",
			"-g", "1",
			"-bf", "0",
			"-pix_fmt", "yuv420p",
			"-movflags", "+faststart",
			outputPath,
		)
	default:
		args = append(args,
			"-c:v", "ffv1",
			"-level", "3",
			"-g", "1",
			"-slices", "24",
			"-slicecrc", "1",
			"-pix_fmt", "bgra",
			outputPath,
		)
	}

	return args, nil
}

func BuildDecodeArgs(config VideoConfig, inputPath string) ([]string, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", inputPath,
		"-vf", fmt.Sprintf("scale=%d:%d:flags=neighbor,format=rgba", config.Width, config.Height),
		"-vsync", "0",
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"pipe:1",
	}, nil
}
