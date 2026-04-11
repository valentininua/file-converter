#!/bin/bash


if [ -z "$1" ]; then
    echo "Usage: ./run.sh [encode|decode] [input file] [output file]"
    echo "docker build -t yt-encoder ."
    echo "docker run --rm -v \"$(pwd)/data:/data\" yt-encoder encode  /data/restored2.zip  /data/restored2.mp4"
    echo "docker run --rm -v \"$(pwd)/data:/data\" yt-encoder decode /data/restored2.mp4 /data/restored2.zip"
    exit 1
fi

CODE="$1"
INPUT="$2"
OUTPUT="$3"

if [ -z "$CODE" || "$CODE" != "encode" || "$CODE" != "decode" ]; then
    CODE="encode or decode"
fi

if [ -z "$INPUT" ]; then
    echo "input file is required"
    exit 1
fi

if [ -z "$OUTPUT" ]; then
    echo "output file is required"
    exit 1
fi
 
 
docker build -t yt-encoder .
docker run --rm -v "$(pwd)/data:/data" yt-encoder $CODE $INPUT $OUTPUT && echo "done" || echo "error"
 


exit 0