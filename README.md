# vtrace

A high-performance CLI tool that measures Time to First Frame (TTFF) for HLS streams. 
vtrace provides granular latency breakdowns to identify bottlenecks in network, 
manifest delivery, segment fetching, or video decoding.

## Features

- Microsecond-accurate network timing via `httptrace`
- DNS, TCP, TLS, and TTFB breakdown
- HLS manifest parsing (master and media playlists)
- First frame detection via ffprobe
- Multi-sample mode with statistical analysis (mean, median, min, max, stddev)
- IQR-based outlier detection with optional exclusion
- Configurable delay between samples (fixed or randomized)
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
vtrace -url <HLS_URL> [flags]
```

### Flags

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--url` | `-u` | HLS stream URL (required) | - |
| `--timeout` | `-t` | Request timeout | 30s |
| `--verbose` | `-v` | Enable verbose output | false |
| `--samples` | `-n` | Number of measurement iterations | 1 |
| `--delay` | `-d` | Fixed delay between samples | 5s |
| `--delay-random` | | Randomized delay range (e.g., 2s-8s) | - |
| `--exclude-outliers` | | Exclude outliers from average calculation | false |

### Examples

Basic usage:
```bash
vtrace -u https://example.com/stream.m3u8
```

Multi-sample with statistics (10 samples, 5s delay):
```bash
vtrace -u https://example.com/stream.m3u8 -n 10
```

Random delay between samples:
```bash
vtrace -u https://example.com/stream.m3u8 -n 5 --delay-random 2s-8s
```

Exclude outliers from average:
```bash
vtrace -u https://example.com/stream.m3u8 -n 10 --exclude-outliers
```

Custom timeout with verbose output:
```bash
vtrace -u https://example.com/stream.m3u8 -t 60s -v
```

## Sample Output

### Single Measurement

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

### Multi-Sample Statistics

```
vtrace results for: https://example.com/stream.m3u8 (5 samples)
──────────────────────────────────────────────────────────────────────────────────
                          Avg          Min          Max       Median       StdDev
──────────────────────────────────────────────────────────────────────────────────
DNS Lookup:            12.34ms       10.21ms       15.67ms       12.11ms        2.10ms
TCP Connect:           45.67ms       42.11ms       49.02ms       45.89ms        2.80ms
TLS Handshake:         89.01ms       85.23ms       94.56ms       88.45ms        3.50ms
Manifest TTFB:         23.45ms       21.02ms       26.78ms       23.12ms        2.10ms
Segment Download:     156.78ms      148.34ms      168.92ms      155.67ms        7.80ms
Frame Detection:       34.56ms       31.23ms       38.90ms       34.12ms        2.90ms
──────────────────────────────────────────────────────────────────────────────────
Total TTFF:           361.81ms      340.12ms      392.45ms      359.36ms       18.30ms

Outliers detected: sample 3 (392.45ms, +8.5%)
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

### Multi-Sample Mode

When running with `-n` greater than 1, vtrace collects multiple measurements and computes aggregate statistics.

**Statistics reported:**

| Metric | Description |
|--------|-------------|
| Avg | Arithmetic mean of all samples |
| Min | Fastest measurement |
| Max | Slowest measurement |
| Median | Middle value (less sensitive to outliers than mean) |
| StdDev | Standard deviation (sample-based, n-1 denominator) |

**Outlier detection:**

vtrace uses the Interquartile Range (IQR) method to identify outliers:

1. Calculate Q1 (25th percentile) and Q3 (75th percentile)
2. Compute IQR = Q3 - Q1
3. Flag samples below Q1 - 1.5×IQR or above Q3 + 1.5×IQR as outliers

Outliers are reported with their deviation from the mean as a percentage. When `--exclude-outliers` is set, flagged samples are excluded from all average calculations and the header shows "Avg*" to indicate filtered results.

**Delay options:**

- `--delay` (default 5s): Fixed wait time between samples. Helps avoid rate limiting and allows CDN cache state to normalize.
- `--delay-random`: Randomized delay within a range (e.g., `2s-8s`). Useful for simulating more realistic access patterns and avoiding cache-friendly timing.

These flags are mutually exclusive. If `--delay-random` is provided, it takes precedence.

## Philosophy

vtrace is built around several design principles:

**User-centric measurement.** TTFF represents the real-world viewer experience—how quickly can a user see the first frame after clicking play? This is the metric that directly impacts perceived video startup latency.

**Granular breakdown.** By exposing each phase (DNS, TCP, TLS, server response, download, decode), vtrace helps identify where the bottleneck lives. Is it the CDN edge? TLS negotiation? Decoder performance? The breakdown tells you where to focus optimization efforts.

**Cold-start accuracy.** vtrace measures fresh connections without HTTP keep-alive or connection pooling. This simulates the initial viewer experience—the worst-case scenario that matters most for first impressions.

**Minimal dependencies.** The tool relies only on `ffprobe` for frame detection, avoiding heavyweight video player dependencies or browser automation. This keeps vtrace fast, portable, and easy to integrate into CI/CD pipelines or monitoring systems.

## License

MIT
