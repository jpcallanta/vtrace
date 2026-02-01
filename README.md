# vtrace

A high-performance CLI tool that measures Time to First Frame (TTFF) for HLS streams. 
vtrace provides granular latency breakdowns to identify bottlenecks in network, 
manifest delivery, segment fetching, or video decoding.

## Features

- Microsecond-accurate network timing via `httptrace`
- DNS, TCP, TLS, and TTFB breakdown
- HLS manifest parsing (master and media playlists)
- First frame detection via ffprobe
- Clean, formatted output

## Requirements

- Go 1.24+
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

### Timing Methodology

vtrace uses Go's `net/http/httptrace` package for microsecond-accurate network timing. The following callback hooks capture precise timestamps during each HTTP request:

| Hook | Measures |
|------|----------|
| `DNSStart` / `DNSDone` | DNS resolution duration |
| `ConnectStart` / `ConnectDone` | TCP connection establishment |
| `TLSHandshakeStart` / `TLSHandshakeDone` | TLS negotiation time |
| `GotFirstResponseByte` | Time to First Byte (TTFB) from request start |

These network phases are discrete intervals within a single request. DNS, TCP, and TLS happen sequentially during connection setup, while TTFB represents the total time from request initiation until the server begins responding.

For frame detection, vtrace pipes the downloaded segment data directly to `ffprobe` using stdin. The `-read_intervals %+#1` flag instructs ffprobe to read only until the first frame is detected, minimizing processing overhead.

### TTFF Calculation

The total Time to First Frame is calculated as:

```
Total TTFF = Manifest Fetch + Segment Download + Frame Detection
```

The breakdown metrics (DNS, TCP, TLS, TTFB) are sub-phases of the Manifest Fetch time and are reported for diagnostic purposes. They are not additive to the total—they represent where time is spent within the manifest request.

### Measurement Flow

1. Fetch the HLS manifest with full network tracing
2. Parse the playlist (follow master → media playlist if needed)
3. Identify and download the first video segment
4. Pipe segment data to ffprobe to detect the first video frame
5. Sum the elapsed times for total TTFF

## Philosophy

vtrace is built around several design principles:

**User-centric measurement.** TTFF represents the real-world viewer experience—how quickly can a user see the first frame after clicking play? This is the metric that directly impacts perceived video startup latency.

**Granular breakdown.** By exposing each phase (DNS, TCP, TLS, server response, download, decode), vtrace helps identify where the bottleneck lives. Is it the CDN edge? TLS negotiation? Decoder performance? The breakdown tells you where to focus optimization efforts.

**Cold-start accuracy.** vtrace measures fresh connections without HTTP keep-alive or connection pooling. This simulates the initial viewer experience—the worst-case scenario that matters most for first impressions.

**Minimal dependencies.** The tool relies only on `ffprobe` for frame detection, avoiding heavyweight video player dependencies or browser automation. This keeps vtrace fast, portable, and easy to integrate into CI/CD pipelines or monitoring systems.

## License

MIT
