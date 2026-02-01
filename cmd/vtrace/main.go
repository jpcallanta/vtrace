package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"codeberg.org/pwnderpants/vtrace/internal/decoder"
	"codeberg.org/pwnderpants/vtrace/internal/probe"
)

func main() {
	urlFlag := flag.String("url", "", "HLS stream URL (required)")
	timeoutFlag := flag.Duration("timeout", 30*time.Second, "Request timeout")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output")

	flag.Parse()

	// Validate required flags
	if *urlFlag == "" {
		fmt.Fprintln(os.Stderr, "error: -url is required")
		flag.Usage()
		os.Exit(1)
	}

	// Check ffprobe availability
	if err := decoder.CheckFFprobe(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
	defer cancel()

	client := probe.NewHTTPClient(*timeoutFlag)

	// Fetch initial playlist
	if *verboseFlag {
		fmt.Printf("Fetching playlist: %s\n", *urlFlag)
	}

	result, err := probe.FetchPlaylist(ctx, *urlFlag, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	manifestTrace := result.Trace

	baseURL, err := probe.GetBaseURL(*urlFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Handle master playlist by fetching media playlist
	if result.Master != nil {
		variantURL, err := probe.GetFirstVariantURL(result.Master, baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		if *verboseFlag {
			fmt.Printf("Fetching media playlist: %s\n", variantURL)
		}

		result, err = probe.FetchPlaylist(ctx, variantURL, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		baseURL, err = probe.GetBaseURL(variantURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	// Get first segment URL
	segmentURL, err := probe.GetFirstSegmentURL(result.Media, baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Printf("Downloading segment: %s\n", segmentURL)
	}

	// Download segment
	segmentData, segmentTrace, err := probe.DownloadSegment(ctx, segmentURL, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *verboseFlag {
		fmt.Println("Detecting first frame...")
	}

	// Detect first frame
	frameDetection, err := decoder.DetectFirstFrame(ctx, segmentData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Calculate total TTFF
	totalTTFF := manifestTrace.Total + segmentTrace.Total + frameDetection

	printResults(*urlFlag, manifestTrace, segmentTrace, frameDetection, totalTTFF)
}

// printResults outputs the timing breakdown to stdout
func printResults(url string, manifest, segment *probe.Trace, frame, total time.Duration) {
	fmt.Printf("vtrace results for: %s\n", url)
	fmt.Println("────────────────────────────────────────────────────")
	fmt.Printf("DNS Lookup:                  %12s\n", formatDuration(manifest.DNSLookup))
	fmt.Printf("TCP Connect:                 %12s\n", formatDuration(manifest.TCPConnect))
	fmt.Printf("TLS Handshake:               %12s\n", formatDuration(manifest.TLSHandshake))
	fmt.Printf("Manifest TTFB:               %12s\n", formatDuration(manifest.TTFB))
	fmt.Printf("Segment Download:            %12s\n", formatDuration(segment.Total))
	fmt.Printf("Frame Detection:             %12s\n", formatDuration(frame))
	fmt.Println("────────────────────────────────────────────────────")
	fmt.Printf("Total TTFF:                  %12s\n", formatDuration(total))
}

// formatDuration formats a duration as milliseconds with 2 decimal places
func formatDuration(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)

	return fmt.Sprintf("%.2fms", ms)
}
