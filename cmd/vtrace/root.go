package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"codeberg.org/pwnderpants/vtrace/internal/decoder"
	"codeberg.org/pwnderpants/vtrace/internal/probe"
)

var (
	url     string
	timeout time.Duration
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "vtrace",
	Short: "Measure Time To First Frame for HLS streams",
	Long: `vtrace measures the Time To First Frame (TTFF) for HLS video streams.

It breaks down the latency into DNS lookup, TCP connect, TLS handshake,
manifest fetch, segment download, and frame detection times.`,
	RunE: run,
}

// init configures the root command flags
func init() {
	rootCmd.Flags().StringVarP(&url, "url", "u", "", "HLS stream URL (required)")
	rootCmd.Flags().DurationVarP(&timeout, "timeout", "t", 30*time.Second, "Request timeout")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	rootCmd.MarkFlagRequired("url")
}

// run executes the main TTFF measurement logic
func run(cmd *cobra.Command, args []string) error {
	// Check ffprobe availability
	if err := decoder.CheckFFprobe(); err != nil {
		return fmt.Errorf("ffprobe check failed: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTPClient(timeout)

	// Fetch initial playlist
	if verbose {
		fmt.Printf("Fetching playlist: %s\n", url)
	}

	result, err := probe.FetchPlaylist(ctx, url, client)
	if err != nil {
		return fmt.Errorf("failed to fetch playlist: %w", err)
	}

	manifestTrace := result.Trace

	baseURL, err := probe.GetBaseURL(url)
	if err != nil {
		return fmt.Errorf("failed to get base URL: %w", err)
	}

	// Handle master playlist by fetching media playlist
	if result.Master != nil {
		variantURL, err := probe.GetFirstVariantURL(result.Master, baseURL)
		if err != nil {
			return fmt.Errorf("failed to get variant URL: %w", err)
		}

		if verbose {
			fmt.Printf("Fetching media playlist: %s\n", variantURL)
		}

		result, err = probe.FetchPlaylist(ctx, variantURL, client)
		if err != nil {
			return fmt.Errorf("failed to fetch media playlist: %w", err)
		}

		baseURL, err = probe.GetBaseURL(variantURL)
		if err != nil {
			return fmt.Errorf("failed to get variant base URL: %w", err)
		}
	}

	segmentURL, err := probe.GetFirstSegmentURL(result.Media, baseURL)
	if err != nil {
		return fmt.Errorf("failed to get segment URL: %w", err)
	}

	if verbose {
		fmt.Printf("Downloading segment: %s\n", segmentURL)
	}

	// Download segment
	segmentData, segmentTrace, err := probe.DownloadSegment(ctx, segmentURL, client)
	if err != nil {
		return fmt.Errorf("failed to download segment: %w", err)
	}

	if verbose {
		fmt.Println("Detecting first frame...")
	}

	// Detect first frame
	frameDetection, err := decoder.DetectFirstFrame(ctx, segmentData)
	if err != nil {
		return fmt.Errorf("failed to detect first frame: %w", err)
	}

	totalTTFF := manifestTrace.Total + segmentTrace.Total + frameDetection

	printResults(url, manifestTrace, segmentTrace, frameDetection, totalTTFF)

	return nil
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
