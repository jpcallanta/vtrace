# vtrace

A high-performance CLI tool that measures Time to First Frame (TTFF) for HLS streams. vtrace provides granular latency breakdowns to identify bottlenecks in network, manifest delivery, segment fetching, or video decoding.

## Features

- Microsecond-accurate network timing via `httptrace`
- DNS, TCP, TLS, and TTFB breakdown
- HLS manifest parsing (master and media playlists)
- First frame detection via ffprobe
- Clean, formatted output

## Requirements

- Go 1.21+
- ffprobe (part of FFmpeg) installed and in PATH

### Installing ffprobe

**Debian/Ubuntu:**
```bash
sudo apt install ffmpeg
```

**macOS:**
```bash
brew install ffmpeg
```

**Fedora:**
```bash
sudo dnf install ffmpeg
```

## Installation

```bash
go install codeberg.org/pwnderpants/vtrace/cmd/vtrace@latest
```

Or build from source:

```bash
git clone https://codeberg.org/pwnderpants/vtrace.git
cd vtrace
go build -o vtrace ./cmd/vtrace
```

## Usage

```bash
vtrace -url <HLS_URL> [-timeout <duration>] [-verbose]
```

### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-url` | HLS stream URL (required) | - |
| `-timeout` | Request timeout | 30s |
| `-verbose` | Enable verbose output | false |

### Examples

Basic usage:
```bash
vtrace -url https://example.com/stream.m3u8
```

With verbose output:
```bash
vtrace -url https://example.com/stream.m3u8 -verbose
```

Custom timeout:
```bash
vtrace -url https://example.com/stream.m3u8 -timeout 60s
```

## Sample Output

```
vtrace results for: https://example.com/stream.m3u8
────────────────────────────────────────────────────
DNS Lookup:                    12.34ms
TCP Connect:                   45.67ms
TLS Handshake:                 89.01ms
Manifest TTFB:                 23.45ms
Segment Download:             156.78ms
Frame Detection:               34.56ms
────────────────────────────────────────────────────
Total TTFF:                   361.81ms
```

## How It Works

1. Starts a global timer at request initiation
2. Fetches the HLS manifest with network tracing (DNS, TCP, TLS, TTFB)
3. Parses master playlist and follows to media playlist if needed
4. Identifies the first video segment
5. Downloads the segment with timing
6. Pipes segment data to ffprobe to detect the first video frame
7. Reports all timing breakdowns

## License

MIT
