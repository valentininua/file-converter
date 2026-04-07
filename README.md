# yt_encoder_project

 
```text
yt_encoder_project/
├─ cmd/
│  └─ main.go
├─ internal/
│  ├─ encoder.go
│  └─ decoder.go
├─ pkg/
│  └─ utils.go
├─ go.mod
├─ Dockerfile
├─ docker-compose.yml
└─ README.md
```

## How It Works

1. The encoder wraps the original file in a small binary envelope with file name, size and SHA-256 checksum.
2. Each byte is split into two 4-bit nibbles.
3. Each nibble is painted as a colored square inside an in-memory RGBA frame.
4. Frames are streamed directly into `ffmpeg`, which stores them using a lossless codec.
5. The decoder requests raw RGBA frames from `ffmpeg`, samples each square, rebuilds the byte stream, validates SHA-256 and writes the exact original file.

## Default Video Settings

- Resolution: `1920x1080`
- Cell size: `8x8`
- FPS: `24`
- Storage density: `16200 bytes/frame`

## Local Usage

Requirements:

- Go `1.22+`
- `ffmpeg` available in `PATH`

Build:

```bash
go build -o yt-encoder ./cmd
```

Encode:

```bash
./yt-encoder encode ./data/input.bin ./data/output.mkv
```

Decode:

```bash
./yt-encoder decode ./data/output.mkv ./data/restored.bin
```

Notes:

- `.mkv` is the recommended output container for the default `FFV1` lossless codec.
- `.mp4` and `.mov` are also supported and use lossless `libx264rgb`.

## Docker

Build image:

```bash
docker build -t yt-encoder .
```

Run encode:

```bash
docker run --rm -v "$(pwd)/data:/data" yt-encoder encode /data/input.bin /data/output.mkv
```

Run decode:

```bash
docker run --rm -v "$(pwd)/data:/data" yt-encoder decode /data/output.mkv /data/restored.bin
```

Using Docker Compose:

```bash
docker compose run --rm yt-encoder encode /data/input.bin /data/output.mkv
docker compose run --rm yt-encoder decode /data/output.mkv /data/restored.bin
```
