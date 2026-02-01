package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"codeberg.org/pwnderpants/vtrace/internal/decoder"
	"codeberg.org/pwnderpants/vtrace/internal/probe"
	"codeberg.org/pwnderpants/vtrace/internal/stats"
)

var (
	url             string
	timeout         time.Duration
	verbose         bool
	samples         int
	delay           time.Duration
	delayRandom     string
	excludeOutliers bool
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
	rootCmd.Flags().IntVarP(&samples, "samples", "n", 1, "Number of measurement iterations")
	rootCmd.Flags().DurationVarP(&delay, "delay", "d", 5*time.Second, "Fixed delay between samples")
	rootCmd.Flags().StringVar(&delayRandom, "delay-random", "", "Randomized delay range (e.g., 2s-8s)")
	rootCmd.Flags().BoolVar(&excludeOutliers, "exclude-outliers", false, "Exclude outliers from average calculation")

	rootCmd.MarkFlagRequired("url")
}

// run executes the main TTFF measurement logic
func run(cmd *cobra.Command, args []string) error {
	// Check ffprobe availability
	if err := decoder.CheckFFprobe(); err != nil {
		return fmt.Errorf("ffprobe check failed: %w", err)
	}

	// Validate samples flag
	if samples < 1 {
		return errors.New("samples must be at least 1")
	}

	// Parse delay-random if provided
	var minDelay, maxDelay time.Duration

	if delayRandom != "" {
		var err error

		minDelay, maxDelay, err = parseDelayRange(delayRandom)
		if err != nil {
			return fmt.Errorf("invalid delay-random format: %w", err)
		}
	}

	// Single sample mode
	if samples == 1 {
		sample, manifestTrace, segmentTrace, err := measureTTFF()
		if err != nil {
			return err
		}

		printResults(url, manifestTrace, segmentTrace, sample.FrameDetection, sample.TotalTTFF)

		return nil
	}

	// Multi-sample mode
	var allSamples []stats.Sample

	for i := 0; i < samples; i++ {
		if verbose {
			fmt.Printf("\n── Sample %d/%d ──\n", i+1, samples)
		}

		sample, _, _, err := measureTTFF()
		if err != nil {
			return fmt.Errorf("sample %d failed: %w", i+1, err)
		}

		allSamples = append(allSamples, sample)

		if verbose {
			fmt.Printf("  TTFF: %s\n", formatDuration(sample.TotalTTFF))
		}

		// Apply delay between samples (skip after last sample)
		if i < samples-1 {
			sleepDuration := getDelay(minDelay, maxDelay)

			if verbose {
				fmt.Printf("  Waiting %s before next sample...\n", sleepDuration)
			}

			time.Sleep(sleepDuration)
		}
	}

	printMultiSampleResults(url, allSamples)

	return nil
}

// measureTTFF performs a single TTFF measurement
func measureTTFF() (stats.Sample, *probe.Trace, *probe.Trace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTPClient(timeout)

	// Fetch initial playlist
	if verbose {
		fmt.Printf("Fetching playlist: %s\n", url)
	}

	result, err := probe.FetchPlaylist(ctx, url, client)
	if err != nil {
		return stats.Sample{}, nil, nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}

	manifestTrace := result.Trace

	baseURL, err := probe.GetBaseURL(url)
	if err != nil {
		return stats.Sample{}, nil, nil, fmt.Errorf("failed to get base URL: %w", err)
	}

	// Handle master playlist by fetching media playlist
	if result.Master != nil {
		variantURL, err := probe.GetFirstVariantURL(result.Master, baseURL)
		if err != nil {
			return stats.Sample{}, nil, nil, fmt.Errorf("failed to get variant URL: %w", err)
		}

		if verbose {
			fmt.Printf("Fetching media playlist: %s\n", variantURL)
		}

		result, err = probe.FetchPlaylist(ctx, variantURL, client)
		if err != nil {
			return stats.Sample{}, nil, nil, fmt.Errorf("failed to fetch media playlist: %w", err)
		}

		baseURL, err = probe.GetBaseURL(variantURL)
		if err != nil {
			return stats.Sample{}, nil, nil, fmt.Errorf("failed to get variant base URL: %w", err)
		}
	}

	segmentURL, err := probe.GetFirstSegmentURL(result.Media, baseURL)
	if err != nil {
		return stats.Sample{}, nil, nil, fmt.Errorf("failed to get segment URL: %w", err)
	}

	if verbose {
		fmt.Printf("Downloading segment: %s\n", segmentURL)
	}

	// Download segment
	segmentData, segmentTrace, err := probe.DownloadSegment(ctx, segmentURL, client)
	if err != nil {
		return stats.Sample{}, nil, nil, fmt.Errorf("failed to download segment: %w", err)
	}

	if verbose {
		fmt.Println("Detecting first frame...")
	}

	// Detect first frame
	frameDetection, err := decoder.DetectFirstFrame(ctx, segmentData)
	if err != nil {
		return stats.Sample{}, nil, nil, fmt.Errorf("failed to detect first frame: %w", err)
	}

	sample := stats.Sample{
		DNSLookup:      manifestTrace.DNSLookup,
		TCPConnect:     manifestTrace.TCPConnect,
		TLSHandshake:   manifestTrace.TLSHandshake,
		ManifestTTFB:   manifestTrace.TTFB,
		ManifestTotal:  manifestTrace.Total,
		SegmentTotal:   segmentTrace.Total,
		FrameDetection: frameDetection,
		TotalTTFF:      manifestTrace.Total + segmentTrace.Total + frameDetection,
	}

	return sample, manifestTrace, segmentTrace, nil
}

// parseDelayRange parses a delay range string like "2s-8s"
func parseDelayRange(rangeStr string) (time.Duration, time.Duration, error) {
	parts := strings.Split(rangeStr, "-")

	if len(parts) != 2 {
		return 0, 0, errors.New("expected format: <min>-<max> (e.g., 2s-8s)")
	}

	minDelay, err := time.ParseDuration(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid min delay: %w", err)
	}

	maxDelay, err := time.ParseDuration(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid max delay: %w", err)
	}

	if minDelay > maxDelay {
		return 0, 0, errors.New("min delay cannot be greater than max delay")
	}

	return minDelay, maxDelay, nil
}

// getDelay returns the delay duration based on configuration
func getDelay(minDelay, maxDelay time.Duration) time.Duration {
	// Check if random delay is configured
	if minDelay > 0 || maxDelay > 0 {
		rangeNs := maxDelay.Nanoseconds() - minDelay.Nanoseconds()

		randomNs := rand.Int63n(rangeNs + 1)

		return minDelay + time.Duration(randomNs)
	}

	return delay
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

// printMultiSampleResults outputs aggregate statistics for multiple samples
func printMultiSampleResults(url string, allSamples []stats.Sample) {
	ttffDurations := stats.ExtractTotalTTFF(allSamples)
	outliers := stats.DetectOutliers(ttffDurations)

	// Determine which durations to use for stats
	durationsForStats := ttffDurations

	if excludeOutliers && len(outliers) > 0 {
		durationsForStats = stats.ExcludeOutliers(ttffDurations, outliers)
	}

	avgLabel := "Avg"

	if excludeOutliers && len(outliers) > 0 {
		avgLabel = "Avg*"
	}

	fmt.Printf("\nvtrace results for: %s (%d samples)\n", url, len(allSamples))
	fmt.Println("──────────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %12s %12s %12s %12s %12s\n", "", avgLabel, "Min", "Max", "Median", "StdDev")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────────")

	printStatRow("DNS Lookup:", stats.ExtractDNSLookup(allSamples), outliers)
	printStatRow("TCP Connect:", stats.ExtractTCPConnect(allSamples), outliers)
	printStatRow("TLS Handshake:", stats.ExtractTLSHandshake(allSamples), outliers)
	printStatRow("Manifest TTFB:", stats.ExtractManifestTTFB(allSamples), outliers)
	printStatRow("Segment Download:", stats.ExtractSegmentTotal(allSamples), outliers)
	printStatRow("Frame Detection:", stats.ExtractFrameDetection(allSamples), outliers)

	fmt.Println("──────────────────────────────────────────────────────────────────────────────────")

	ttffStats := stats.ComputeStats(durationsForStats)

	fmt.Printf("%-20s %12s %12s %12s %12s %12s\n",
		"Total TTFF:",
		formatDuration(ttffStats.Mean),
		formatDuration(ttffStats.Min),
		formatDuration(ttffStats.Max),
		formatDuration(ttffStats.Median),
		formatDuration(ttffStats.StdDev),
	)

	// Print outlier information
	if len(outliers) > 0 {
		fmt.Println()

		if excludeOutliers {
			fmt.Print("* Outliers excluded from average: ")
		} else {
			fmt.Print("Outliers detected: ")
		}

		for i, o := range outliers {
			if i > 0 {
				fmt.Print(", ")
			}

			sign := "+"

			if o.Deviation < 0 {
				sign = ""
			}

			fmt.Printf("sample %d (%s, %s%.1f%%)", o.Index+1, formatDuration(o.Value), sign, o.Deviation)
		}

		fmt.Println()
	}
}

// printStatRow prints a single row of statistics
func printStatRow(label string, durations []time.Duration, outliers []stats.Outlier) {
	durationsForStats := durations

	if excludeOutliers && len(outliers) > 0 {
		durationsForStats = stats.ExcludeOutliers(durations, outliers)
	}

	s := stats.ComputeStats(durationsForStats)

	fmt.Printf("%-20s %12s %12s %12s %12s %12s\n",
		label,
		formatDuration(s.Mean),
		formatDuration(s.Min),
		formatDuration(s.Max),
		formatDuration(s.Median),
		formatDuration(s.StdDev),
	)
}

// formatDuration formats a duration as milliseconds with 2 decimal places
func formatDuration(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)

	return fmt.Sprintf("%.2fms", ms)
}
