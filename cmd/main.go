package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	core "ytencoder/internal"
	utils "ytencoder/pkg"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return 1
	}

	switch args[0] {
	case "encode":
		return runEncode(args[1:])
	case "decode":
		return runDecode(args[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage(os.Stderr)
		return 1
	}
}

func runEncode(args []string) int {
	flags := flag.NewFlagSet("encode", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	workers := flags.Int("workers", 0, "number of concurrent frame workers (default: NumCPU)")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 2 {
		printEncodeUsage(os.Stderr)
		return 1
	}

	encoder, err := core.NewEncoder(utils.DefaultConfig(), *workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoder init error: %v\n", err)
		return 1
	}

	result, err := encoder.Encode(flags.Arg(0), flags.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
		return 1
	}

	fmt.Printf(
		"Encoded %d bytes into %d frames -> %s\n",
		result.PayloadBytes,
		result.Frames,
		result.OutputFile,
	)
	return 0
}

func runDecode(args []string) int {
	flags := flag.NewFlagSet("decode", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	workers := flags.Int("workers", 0, "number of concurrent frame workers (default: NumCPU)")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 2 {
		printDecodeUsage(os.Stderr)
		return 1
	}

	decoder, err := core.NewDecoder(utils.DefaultConfig(), *workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decoder init error: %v\n", err)
		return 1
	}

	result, err := decoder.Decode(flags.Arg(0), flags.Arg(1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode error: %v\n", err)
		return 1
	}

	fmt.Printf(
		"Decoded %d bytes from %d frames -> %s (original name: %s)\n",
		result.PayloadBytes,
		result.Frames,
		result.OutputFile,
		result.OriginalFile,
	)
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  yt-encoder encode [--workers N] <input-file> <output-video>")
	fmt.Fprintln(writer, "  yt-encoder decode [--workers N] <input-video> <output-file>")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Notes:")
	fmt.Fprintln(writer, "  - Encoder streams raw RGBA frames directly into ffmpeg.")
	fmt.Fprintln(writer, "  - Decoder reads raw RGBA frames back from ffmpeg stdout.")
	fmt.Fprintln(writer, "  - .mkv is recommended for the fastest lossless workflow; .mp4 uses lossless H.264 RGB.")
}

func printEncodeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: yt-encoder encode [--workers N] <input-file> <output-video>")
}

func printDecodeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: yt-encoder decode [--workers N] <input-video> <output-file>")
}
