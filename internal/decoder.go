package internal

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"sync"

	utils "ytencoder/pkg"
)

type Decoder struct {
	config        utils.VideoConfig
	workers       int
	bytesPerFrame int
	sampleOffsets []int
}

type DecodeResult struct {
	InputFile    string
	OutputFile   string
	OriginalFile string
	PayloadBytes int
	Frames       int
}

type decodeJob struct {
	index int
	frame []byte
}

type decodeResult struct {
	index   int
	payload []byte
	err     error
}

type envelopeWriter struct {
	outputPath   string
	buffer       []byte
	file         *os.File
	header       utils.EnvelopeHeader
	headerParsed bool
	bytesWritten uint64
	hasher       hash.Hash
}

func NewDecoder(config utils.VideoConfig, workers int) (*Decoder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Decoder{
		config:        config,
		workers:       utils.WorkerCount(workers),
		bytesPerFrame: config.BytesPerFrame(),
		sampleOffsets: buildSampleOffsets(config),
	}, nil
}

func (d *Decoder) Decode(inputPath, outputPath string) (DecodeResult, error) {
	args, err := utils.BuildDecodeArgs(d.config, inputPath)
	if err != nil {
		return DecodeResult{}, fmt.Errorf("build ffmpeg decode args: %w", err)
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return DecodeResult{}, fmt.Errorf("open ffmpeg stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return DecodeResult{}, fmt.Errorf("ffmpeg is not installed or not available in PATH")
		}
		return DecodeResult{}, fmt.Errorf("start ffmpeg: %w", err)
	}

	jobs := make(chan decodeJob, d.workers*2)
	results := make(chan decodeResult, d.workers*2)
	readErrCh := make(chan error, 1)

	go d.streamFrames(stdout, jobs, readErrCh)

	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < d.workers; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				payload, decodeErr := d.decodeFrame(job.frame)
				results <- decodeResult{
					index:   job.index,
					payload: payload,
					err:     decodeErr,
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	writer := newEnvelopeWriter(outputPath)
	var firstErr error
	nextFrame := 0
	frames := 0
	pending := make(map[int][]byte, d.workers*2)

	for result := range results {
		if firstErr != nil {
			continue
		}
		if result.err != nil {
			firstErr = result.err
			continue
		}

		pending[result.index] = result.payload
		for {
			payload, ok := pending[nextFrame]
			if !ok {
				break
			}
			delete(pending, nextFrame)

			if err := writer.Consume(payload); err != nil {
				firstErr = err
				break
			}

			nextFrame++
			frames++
		}
	}

	if readErr := <-readErrCh; firstErr == nil && readErr != nil {
		firstErr = readErr
	}
	if waitErr := cmd.Wait(); firstErr == nil && waitErr != nil {
		firstErr = fmt.Errorf("ffmpeg decode failed: %w", waitErr)
	}

	header, finalizeErr := writer.Finalize()
	if firstErr == nil && finalizeErr != nil {
		firstErr = finalizeErr
	}
	if firstErr != nil {
		writer.Abort()
		return DecodeResult{}, firstErr
	}

	return DecodeResult{
		InputFile:    inputPath,
		OutputFile:   outputPath,
		OriginalFile: header.FileName,
		PayloadBytes: int(header.Size),
		Frames:       frames,
	}, nil
}

func buildSampleOffsets(config utils.VideoConfig) []int {
	offsets := make([]int, 0, config.CellsPerFrame())
	center := config.CellSize / 2

	for cellY := 0; cellY < config.BlocksY(); cellY++ {
		baseY := (cellY*config.CellSize + center) * config.Width * 4
		for cellX := 0; cellX < config.BlocksX(); cellX++ {
			offsets = append(offsets, baseY+(cellX*config.CellSize+center)*4)
		}
	}

	return offsets
}

func (d *Decoder) streamFrames(reader io.Reader, jobs chan<- decodeJob, readErrCh chan<- error) {
	defer close(jobs)

	frameSize := d.config.FrameSizeBytes()
	for frameIndex := 0; ; frameIndex++ {
		frame := make([]byte, frameSize)
		readBytes, err := io.ReadFull(reader, frame)
		if err != nil {
			if errors.Is(err, io.EOF) || (errors.Is(err, io.ErrUnexpectedEOF) && readBytes == 0) {
				readErrCh <- nil
				return
			}
			readErrCh <- fmt.Errorf("read raw frame %d: %w", frameIndex, err)
			return
		}

		jobs <- decodeJob{
			index: frameIndex,
			frame: frame,
		}
	}
}

func (d *Decoder) decodeFrame(frame []byte) ([]byte, error) {
	payload := make([]byte, d.bytesPerFrame)
	cellIndex := 0

	for byteIndex := 0; byteIndex < d.bytesPerFrame; byteIndex++ {
		high := d.sampleNibble(frame, cellIndex)
		low := d.sampleNibble(frame, cellIndex+1)
		payload[byteIndex] = utils.NibblesToByte(high, low)
		cellIndex += 2
	}

	return payload, nil
}

func (d *Decoder) sampleNibble(frame []byte, cellIndex int) uint8 {
	offset := d.sampleOffsets[cellIndex]
	return utils.ClosestPaletteIndex(frame[offset], frame[offset+1], frame[offset+2])
}

func newEnvelopeWriter(outputPath string) *envelopeWriter {
	return &envelopeWriter{
		outputPath: outputPath,
		hasher:     sha256.New(),
	}
}

func (w *envelopeWriter) Consume(data []byte) error {
	if w.headerParsed {
		return w.writePayload(data)
	}

	w.buffer = append(w.buffer, data...)
	header, headerSize, err := utils.ParseEnvelopeHeader(w.buffer)
	if errors.Is(err, utils.ErrIncompleteHeader) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("parse envelope header: %w", err)
	}

	file, err := os.Create(w.outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}

	w.file = file
	w.header = header
	w.headerParsed = true

	payload := w.buffer[headerSize:]
	w.buffer = nil
	return w.writePayload(payload)
}

func (w *envelopeWriter) writePayload(data []byte) error {
	if len(data) == 0 || w.bytesWritten >= w.header.Size {
		return nil
	}

	remaining := int(w.header.Size - w.bytesWritten)
	if len(data) > remaining {
		data = data[:remaining]
	}

	if err := writeAll(w.file, data); err != nil {
		return fmt.Errorf("write decoded payload: %w", err)
	}
	if _, err := w.hasher.Write(data); err != nil {
		return fmt.Errorf("hash decoded payload: %w", err)
	}

	w.bytesWritten += uint64(len(data))
	return nil
}

func (w *envelopeWriter) Finalize() (utils.EnvelopeHeader, error) {
	if w.file != nil {
		defer w.file.Close()
	}

	if !w.headerParsed {
		return utils.EnvelopeHeader{}, fmt.Errorf("decoded stream does not contain a valid header")
	}
	if w.bytesWritten != w.header.Size {
		return utils.EnvelopeHeader{}, fmt.Errorf("decoded payload size mismatch: got %d, want %d", w.bytesWritten, w.header.Size)
	}
	if !bytes.Equal(w.hasher.Sum(nil), w.header.Hash[:]) {
		return utils.EnvelopeHeader{}, fmt.Errorf("decoded payload hash mismatch")
	}

	return w.header, nil
}

func (w *envelopeWriter) Abort() {
	if w.file == nil {
		return
	}

	filePath := w.file.Name()
	_ = w.file.Close()
	_ = os.Remove(filePath)
	w.file = nil
}
