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
	compare         bool
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
	rootCmd.Flags().BoolVar(&compare, "compare", false, "Compare HTTP/1.1-2 vs HTTP/3 TTFF timings")

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

	// Handle comparison mode
	if compare {
		return runCompare(minDelay, maxDelay)
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

// runCompare executes comparison mode between HTTP/1.1-2 and HTTP/3 for full TTFF
func runCompare(minDelay, maxDelay time.Duration) error {
	// Single sample comparison mode
	if samples == 1 {
		if verbose {
			fmt.Println("── HTTP/1.1-2 TTFF Measurement ──")
		}

		http12Sample, http12ManifestTrace, http12SegmentTrace, err := measureTTFF()
		if err != nil {
			return fmt.Errorf("HTTP/1.1-2 measurement failed: %w", err)
		}

		if verbose {
			fmt.Println("\n── HTTP/3 TTFF Measurement ──")
		}

		http3Sample, http3ManifestTrace, http3SegmentTrace, err := measureTTFFHTTP3()
		if err != nil {
			return fmt.Errorf("HTTP/3 measurement failed: %w", err)
		}

		printTTFFComparisonResults(url, http12Sample, http3Sample, http12ManifestTrace, http3ManifestTrace, http12SegmentTrace, http3SegmentTrace)

		return nil
	}

	// Multi-sample comparison mode
	var http12Samples []stats.Sample

	var http3Samples []stats.Sample

	// Collect HTTP/1.1-2 samples
	if verbose {
		fmt.Println("\n══ HTTP/1.1-2 TTFF Samples ══")
	}

	for i := 0; i < samples; i++ {
		if verbose {
			fmt.Printf("\n── Sample %d/%d ──\n", i+1, samples)
		}

		sample, _, _, err := measureTTFF()
		if err != nil {
			return fmt.Errorf("HTTP/1.1-2 sample %d failed: %w", i+1, err)
		}

		http12Samples = append(http12Samples, sample)

		if verbose {
			fmt.Printf("  TTFF: %s\n", formatDuration(sample.TotalTTFF))
		}

		// Apply delay between samples
		if i < samples-1 {
			sleepDuration := getDelay(minDelay, maxDelay)

			if verbose {
				fmt.Printf("  Waiting %s before next sample...\n", sleepDuration)
			}

			time.Sleep(sleepDuration)
		}
	}

	// Collect HTTP/3 samples
	if verbose {
		fmt.Println("\n══ HTTP/3 TTFF Samples ══")
	}

	for i := 0; i < samples; i++ {
		if verbose {
			fmt.Printf("\n── Sample %d/%d ──\n", i+1, samples)
		}

		sample, _, _, err := measureTTFFHTTP3()
		if err != nil {
			return fmt.Errorf("HTTP/3 sample %d failed: %w", i+1, err)
		}

		http3Samples = append(http3Samples, sample)

		if verbose {
			fmt.Printf("  TTFF: %s\n", formatDuration(sample.TotalTTFF))
		}

		// Apply delay between samples
		if i < samples-1 {
			sleepDuration := getDelay(minDelay, maxDelay)

			if verbose {
				fmt.Printf("  Waiting %s before next sample...\n", sleepDuration)
			}

			time.Sleep(sleepDuration)
		}
	}

	printMultiSampleTTFFComparisonResults(url, http12Samples, http3Samples)

	return nil
}

// measureManifestTTFB fetches the manifest using HTTP/1.1-2 and returns timing
func measureManifestTTFB() (*probe.Trace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTPClient(timeout)

	if verbose {
		fmt.Printf("Fetching manifest: %s\n", url)
	}

	result, err := probe.FetchPlaylist(ctx, url, client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}

	return result.Trace, nil
}

// measureManifestTTFBHTTP3 fetches the manifest using HTTP/3 and returns timing
func measureManifestTTFBHTTP3() (*probe.Trace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTP3Client(timeout)

	if verbose {
		fmt.Printf("Fetching manifest (HTTP/3): %s\n", url)
	}

	result, err := probe.FetchPlaylistHTTP3(ctx, url, client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}

	return result.Trace, nil
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
		QUICHandshake:  0,
		ManifestTTFB:   manifestTrace.TTFB,
		ManifestTotal:  manifestTrace.Total,
		SegmentTotal:   segmentTrace.Total,
		FrameDetection: frameDetection,
		TotalTTFF:      manifestTrace.Total + segmentTrace.Total + frameDetection,
	}

	return sample, manifestTrace, segmentTrace, nil
}

// measureTTFFHTTP3 performs a single TTFF measurement using HTTP/3
func measureTTFFHTTP3() (stats.Sample, *probe.Trace, *probe.Trace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTP3Client(timeout)

	// Fetch initial playlist
	if verbose {
		fmt.Printf("Fetching playlist (HTTP/3): %s\n", url)
	}

	result, err := probe.FetchPlaylistHTTP3(ctx, url, client)
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
			fmt.Printf("Fetching media playlist (HTTP/3): %s\n", variantURL)
		}

		result, err = probe.FetchPlaylistHTTP3(ctx, variantURL, client)
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
		fmt.Printf("Downloading segment (HTTP/3): %s\n", segmentURL)
	}

	// Download segment
	segmentData, segmentTrace, err := probe.DownloadSegmentHTTP3(ctx, segmentURL, client)
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
		TCPConnect:     0,
		TLSHandshake:   0,
		QUICHandshake:  manifestTrace.QUICHandshake,
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

// formatDelta formats the difference between two durations with sign
func formatDelta(http12, http3 time.Duration) string {
	delta := http3 - http12
	ms := float64(delta) / float64(time.Millisecond)

	if delta >= 0 {
		return fmt.Sprintf("+%.2fms", ms)
	}

	return fmt.Sprintf("%.2fms", ms)
}

// printManifestComparisonResults outputs side-by-side HTTP/1.1-2 vs HTTP/3 manifest comparison
func printManifestComparisonResults(url string, http12Trace, http3Trace *probe.Trace) {
	fmt.Printf("vtrace manifest comparison for: %s\n", url)
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n", "", "HTTP/1.1-2", "HTTP/3", "Delta")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n",
		"DNS Lookup:",
		formatDuration(http12Trace.DNSLookup),
		formatDuration(http3Trace.DNSLookup),
		formatDelta(http12Trace.DNSLookup, http3Trace.DNSLookup),
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"TCP Connect:",
		formatDuration(http12Trace.TCPConnect),
		"N/A",
		"N/A",
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"TLS Handshake:",
		formatDuration(http12Trace.TLSHandshake),
		"N/A",
		"N/A",
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"QUIC Handshake:",
		"N/A",
		formatDuration(http3Trace.QUICHandshake),
		"N/A",
	)
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n",
		"Manifest TTFB:",
		formatDuration(http12Trace.TTFB),
		formatDuration(http3Trace.TTFB),
		formatDelta(http12Trace.TTFB, http3Trace.TTFB),
	)
}

// printMultiSampleManifestComparisonResults outputs aggregate stats for HTTP/1.1-2 vs HTTP/3 manifest
func printMultiSampleManifestComparisonResults(url string, http12Traces, http3Traces []*probe.Trace) {
	fmt.Printf("\nvtrace manifest comparison for: %s (%d samples each)\n", url, len(http12Traces))
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n", "", "HTTP/1.1-2", "HTTP/3", "Delta")
	fmt.Println("────────────────────────────────────────────────────────────────────")

	// Extract durations from traces
	http12DNS := extractTraceDurations(http12Traces, func(t *probe.Trace) time.Duration { return t.DNSLookup })
	http3DNS := extractTraceDurations(http3Traces, func(t *probe.Trace) time.Duration { return t.DNSLookup })

	http12DNSStats := stats.ComputeStats(http12DNS)
	http3DNSStats := stats.ComputeStats(http3DNS)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"DNS Lookup:",
		formatDuration(http12DNSStats.Mean),
		formatDuration(http3DNSStats.Mean),
		formatDelta(http12DNSStats.Mean, http3DNSStats.Mean),
	)

	// TCP Connect (HTTP/1.1-2 only)
	http12TCP := extractTraceDurations(http12Traces, func(t *probe.Trace) time.Duration { return t.TCPConnect })
	http12TCPStats := stats.ComputeStats(http12TCP)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"TCP Connect:",
		formatDuration(http12TCPStats.Mean),
		"N/A",
		"N/A",
	)

	// TLS Handshake (HTTP/1.1-2 only)
	http12TLS := extractTraceDurations(http12Traces, func(t *probe.Trace) time.Duration { return t.TLSHandshake })
	http12TLSStats := stats.ComputeStats(http12TLS)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"TLS Handshake:",
		formatDuration(http12TLSStats.Mean),
		"N/A",
		"N/A",
	)

	// QUIC Handshake (HTTP/3 only)
	http3QUIC := extractTraceDurations(http3Traces, func(t *probe.Trace) time.Duration { return t.QUICHandshake })
	http3QUICStats := stats.ComputeStats(http3QUIC)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"QUIC Handshake:",
		"N/A",
		formatDuration(http3QUICStats.Mean),
		"N/A",
	)

	fmt.Println("────────────────────────────────────────────────────────────────────")

	// Manifest TTFB
	http12TTFB := extractTraceDurations(http12Traces, func(t *probe.Trace) time.Duration { return t.TTFB })
	http3TTFB := extractTraceDurations(http3Traces, func(t *probe.Trace) time.Duration { return t.TTFB })

	http12TTFBStats := stats.ComputeStats(http12TTFB)
	http3TTFBStats := stats.ComputeStats(http3TTFB)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"Manifest TTFB:",
		formatDuration(http12TTFBStats.Mean),
		formatDuration(http3TTFBStats.Mean),
		formatDelta(http12TTFBStats.Mean, http3TTFBStats.Mean),
	)
}

// extractTraceDurations extracts durations from traces using the provided extractor function
func extractTraceDurations(traces []*probe.Trace, extractor func(*probe.Trace) time.Duration) []time.Duration {
	durations := make([]time.Duration, len(traces))

	for i, t := range traces {
		durations[i] = extractor(t)
	}

	return durations
}

// printTTFFComparisonResults outputs side-by-side HTTP/1.1-2 vs HTTP/3 TTFF comparison
func printTTFFComparisonResults(url string, http12Sample, http3Sample stats.Sample, http12Manifest, http3Manifest, http12Segment, http3Segment *probe.Trace) {
	fmt.Printf("vtrace TTFF comparison for: %s\n", url)
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n", "", "HTTP/1.1-2", "HTTP/3", "Delta")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n",
		"DNS Lookup:",
		formatDuration(http12Manifest.DNSLookup),
		formatDuration(http3Manifest.DNSLookup),
		formatDelta(http12Manifest.DNSLookup, http3Manifest.DNSLookup),
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"TCP Connect:",
		formatDuration(http12Manifest.TCPConnect),
		"N/A",
		"N/A",
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"TLS Handshake:",
		formatDuration(http12Manifest.TLSHandshake),
		"N/A",
		"N/A",
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"QUIC Handshake:",
		"N/A",
		formatDuration(http3Manifest.QUICHandshake),
		"N/A",
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"Manifest TTFB:",
		formatDuration(http12Manifest.TTFB),
		formatDuration(http3Manifest.TTFB),
		formatDelta(http12Manifest.TTFB, http3Manifest.TTFB),
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"Segment Download:",
		formatDuration(http12Segment.Total),
		formatDuration(http3Segment.Total),
		formatDelta(http12Segment.Total, http3Segment.Total),
	)
	fmt.Printf("%-20s %14s %14s %14s\n",
		"Frame Detection:",
		formatDuration(http12Sample.FrameDetection),
		formatDuration(http3Sample.FrameDetection),
		formatDelta(http12Sample.FrameDetection, http3Sample.FrameDetection),
	)
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n",
		"Total TTFF:",
		formatDuration(http12Sample.TotalTTFF),
		formatDuration(http3Sample.TotalTTFF),
		formatDelta(http12Sample.TotalTTFF, http3Sample.TotalTTFF),
	)
}

// printMultiSampleTTFFComparisonResults outputs aggregate stats for HTTP/1.1-2 vs HTTP/3 TTFF
func printMultiSampleTTFFComparisonResults(url string, http12Samples, http3Samples []stats.Sample) {
	fmt.Printf("\nvtrace TTFF comparison for: %s (%d samples each)\n", url, len(http12Samples))
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n", "", "HTTP/1.1-2", "HTTP/3", "Delta")
	fmt.Println("────────────────────────────────────────────────────────────────────")

	// DNS Lookup
	http12DNS := stats.ExtractDNSLookup(http12Samples)
	http3DNS := stats.ExtractDNSLookup(http3Samples)

	http12DNSStats := stats.ComputeStats(http12DNS)
	http3DNSStats := stats.ComputeStats(http3DNS)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"DNS Lookup:",
		formatDuration(http12DNSStats.Mean),
		formatDuration(http3DNSStats.Mean),
		formatDelta(http12DNSStats.Mean, http3DNSStats.Mean),
	)

	// TCP Connect (HTTP/1.1-2 only)
	http12TCP := stats.ExtractTCPConnect(http12Samples)
	http12TCPStats := stats.ComputeStats(http12TCP)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"TCP Connect:",
		formatDuration(http12TCPStats.Mean),
		"N/A",
		"N/A",
	)

	// TLS Handshake (HTTP/1.1-2 only)
	http12TLS := stats.ExtractTLSHandshake(http12Samples)
	http12TLSStats := stats.ComputeStats(http12TLS)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"TLS Handshake:",
		formatDuration(http12TLSStats.Mean),
		"N/A",
		"N/A",
	)

	// QUIC Handshake (HTTP/3 only)
	http3QUIC := stats.ExtractQUICHandshake(http3Samples)
	http3QUICStats := stats.ComputeStats(http3QUIC)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"QUIC Handshake:",
		"N/A",
		formatDuration(http3QUICStats.Mean),
		"N/A",
	)

	// Manifest TTFB
	http12TTFB := stats.ExtractManifestTTFB(http12Samples)
	http3TTFB := stats.ExtractManifestTTFB(http3Samples)

	http12TTFBStats := stats.ComputeStats(http12TTFB)
	http3TTFBStats := stats.ComputeStats(http3TTFB)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"Manifest TTFB:",
		formatDuration(http12TTFBStats.Mean),
		formatDuration(http3TTFBStats.Mean),
		formatDelta(http12TTFBStats.Mean, http3TTFBStats.Mean),
	)

	// Segment Download
	http12Segment := stats.ExtractSegmentTotal(http12Samples)
	http3Segment := stats.ExtractSegmentTotal(http3Samples)

	http12SegmentStats := stats.ComputeStats(http12Segment)
	http3SegmentStats := stats.ComputeStats(http3Segment)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"Segment Download:",
		formatDuration(http12SegmentStats.Mean),
		formatDuration(http3SegmentStats.Mean),
		formatDelta(http12SegmentStats.Mean, http3SegmentStats.Mean),
	)

	// Frame Detection
	http12Frame := stats.ExtractFrameDetection(http12Samples)
	http3Frame := stats.ExtractFrameDetection(http3Samples)

	http12FrameStats := stats.ComputeStats(http12Frame)
	http3FrameStats := stats.ComputeStats(http3Frame)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"Frame Detection:",
		formatDuration(http12FrameStats.Mean),
		formatDuration(http3FrameStats.Mean),
		formatDelta(http12FrameStats.Mean, http3FrameStats.Mean),
	)

	fmt.Println("────────────────────────────────────────────────────────────────────")

	// Total TTFF
	http12Total := stats.ExtractTotalTTFF(http12Samples)
	http3Total := stats.ExtractTotalTTFF(http3Samples)

	http12TotalStats := stats.ComputeStats(http12Total)
	http3TotalStats := stats.ComputeStats(http3Total)

	fmt.Printf("%-20s %14s %14s %14s\n",
		"Total TTFF:",
		formatDuration(http12TotalStats.Mean),
		formatDuration(http3TotalStats.Mean),
		formatDelta(http12TotalStats.Mean, http3TotalStats.Mean),
	)
}
