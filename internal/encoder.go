package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	utils "ytencoder/pkg"
)

type Encoder struct {
	config      utils.VideoConfig
	workers     int
	rowStride   int
	cellOffsets []int
	cellRows    [16][]byte
}

type EncodeResult struct {
	InputFile    string
	OutputFile   string
	PayloadBytes int
	Frames       int
}

type encodeJob struct {
	index   int
	payload []byte
}

type encodeResult struct {
	index int
	frame []byte
	err   error
}

func NewEncoder(config utils.VideoConfig, workers int) (*Encoder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &Encoder{
		config:      config,
		workers:     utils.WorkerCount(workers),
		rowStride:   config.Width * 4,
		cellOffsets: buildCellOffsets(config),
		cellRows:    buildCellRows(config.CellSize),
	}, nil
}

func (e *Encoder) Encode(inputPath, outputPath string) (EncodeResult, error) {
	payload, err := os.ReadFile(inputPath)
	if err != nil {
		return EncodeResult{}, fmt.Errorf("read input file: %w", err)
	}

	envelope, err := utils.BuildEnvelope(inputPath, payload)
	if err != nil {
		return EncodeResult{}, fmt.Errorf("build binary envelope: %w", err)
	}

	frames := (len(envelope) + e.config.BytesPerFrame() - 1) / e.config.BytesPerFrame()

	args, err := utils.BuildEncodeArgs(e.config, outputPath)
	if err != nil {
		return EncodeResult{}, fmt.Errorf("build ffmpeg encode args: %w", err)
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return EncodeResult{}, fmt.Errorf("open ffmpeg stdin: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return EncodeResult{}, fmt.Errorf("ffmpeg is not installed or not available in PATH")
		}
		return EncodeResult{}, fmt.Errorf("start ffmpeg: %w", err)
	}

	jobs := make(chan encodeJob, e.workers*2)
	results := make(chan encodeResult, e.workers*2)

	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < e.workers; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				frame, renderErr := e.renderFrame(job.payload)
				results <- encodeResult{
					index: job.index,
					frame: frame,
					err:   renderErr,
				}
			}
		}()
	}

	go func() {
		for frameIndex := 0; frameIndex < frames; frameIndex++ {
			start := frameIndex * e.config.BytesPerFrame()
			end := start + e.config.BytesPerFrame()
			if end > len(envelope) {
				end = len(envelope)
			}

			jobs <- encodeJob{
				index:   frameIndex,
				payload: envelope[start:end],
			}
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	nextFrame := 0
	pending := make(map[int][]byte, e.workers*2)

	for result := range results {
		if firstErr != nil {
			continue
		}
		if result.err != nil {
			firstErr = result.err
			continue
		}

		pending[result.index] = result.frame
		for {
			frame, ok := pending[nextFrame]
			if !ok {
				break
			}
			delete(pending, nextFrame)

			if err := writeAll(stdin, frame); err != nil {
				firstErr = fmt.Errorf("write frame %d to ffmpeg: %w", nextFrame, err)
				break
			}

			nextFrame++
		}
	}

	if closeErr := stdin.Close(); firstErr == nil && closeErr != nil {
		firstErr = fmt.Errorf("close ffmpeg stdin: %w", closeErr)
	}
	if waitErr := cmd.Wait(); firstErr == nil && waitErr != nil {
		firstErr = fmt.Errorf("ffmpeg encode failed: %w", waitErr)
	}
	if firstErr != nil {
		return EncodeResult{}, firstErr
	}

	return EncodeResult{
		InputFile:    inputPath,
		OutputFile:   outputPath,
		PayloadBytes: len(payload),
		Frames:       frames,
	}, nil
}

func buildCellOffsets(config utils.VideoConfig) []int {
	offsets := make([]int, 0, config.CellsPerFrame())
	rowStride := config.Width * 4

	for cellY := 0; cellY < config.BlocksY(); cellY++ {
		baseY := cellY * config.CellSize * rowStride
		for cellX := 0; cellX < config.BlocksX(); cellX++ {
			offsets = append(offsets, baseY+cellX*config.CellSize*4)
		}
	}

	return offsets
}

func buildCellRows(cellSize int) [16][]byte {
	var rows [16][]byte
	for index, clr := range utils.Palette {
		row := make([]byte, cellSize*4)
		for pixel := 0; pixel < cellSize; pixel++ {
			offset := pixel * 4
			row[offset] = clr[0]
			row[offset+1] = clr[1]
			row[offset+2] = clr[2]
			row[offset+3] = clr[3]
		}
		rows[index] = row
	}
	return rows
}

func (e *Encoder) renderFrame(payload []byte) ([]byte, error) {
	frame := make([]byte, e.config.FrameSizeBytes())

	for byteIndex, value := range payload {
		high, low := utils.ByteToNibbles(value)
		e.paintCell(frame, byteIndex*2, high)
		e.paintCell(frame, byteIndex*2+1, low)
	}

	return frame, nil
}

func (e *Encoder) paintCell(frame []byte, cellIndex int, paletteIndex uint8) {
	baseOffset := e.cellOffsets[cellIndex]
	row := e.cellRows[paletteIndex]

	for rowIndex := 0; rowIndex < e.config.CellSize; rowIndex++ {
		start := baseOffset + rowIndex*e.rowStride
		copy(frame[start:start+len(row)], row)
	}
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		data = data[written:]
	}
	return nil
}
