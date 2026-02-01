package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/spf13/cobra"

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
	Use:   "atrace",
	Short: "Measure Time To First Byte for any asset",
	Long: `atrace measures the Time To First Byte (TTFB) for any HTTP asset.

It breaks down the latency into DNS lookup, TCP connect, TLS handshake,
and time to first byte.`,
	RunE: run,
}

// init configures the root command flags
func init() {
	rootCmd.Flags().StringVarP(&url, "url", "u", "", "Asset URL (required)")
	rootCmd.Flags().DurationVarP(&timeout, "timeout", "t", 30*time.Second, "Request timeout")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.Flags().IntVarP(&samples, "samples", "n", 1, "Number of measurement iterations")
	rootCmd.Flags().DurationVarP(&delay, "delay", "d", 5*time.Second, "Fixed delay between samples")
	rootCmd.Flags().StringVar(&delayRandom, "delay-random", "", "Randomized delay range (e.g., 2s-8s)")
	rootCmd.Flags().BoolVar(&excludeOutliers, "exclude-outliers", false, "Exclude outliers from average calculation")
	rootCmd.Flags().BoolVar(&compare, "compare", false, "Compare HTTP/1.1-2 vs HTTP/3 timings")

	rootCmd.MarkFlagRequired("url")
}

// run executes the main TTFB measurement logic
func run(cmd *cobra.Command, args []string) error {
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
		sample, trace, err := measureTTFB()
		if err != nil {
			return err
		}

		printResults(url, trace, sample.TTFB)

		return nil
	}

	// Multi-sample mode
	var allSamples []stats.AssetSample

	for i := 0; i < samples; i++ {
		if verbose {
			fmt.Printf("\n── Sample %d/%d ──\n", i+1, samples)
		}

		sample, _, err := measureTTFB()
		if err != nil {
			return fmt.Errorf("sample %d failed: %w", i+1, err)
		}

		allSamples = append(allSamples, sample)

		if verbose {
			fmt.Printf("  TTFB: %s\n", formatDuration(sample.TTFB))
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

// runCompare executes comparison mode between HTTP/1.1-2 and HTTP/3
func runCompare(minDelay, maxDelay time.Duration) error {
	// Single sample comparison mode
	if samples == 1 {
		if verbose {
			fmt.Println("── HTTP/1.1-2 ──")
		}

		http12Sample, http12Trace, err := measureTTFB()
		if err != nil {
			return fmt.Errorf("HTTP/1.1-2 measurement failed: %w", err)
		}

		if verbose {
			fmt.Println("\n── HTTP/3 ──")
		}

		http3Sample, http3Trace, err := measureTTFBHTTP3()
		if err != nil {
			return fmt.Errorf("HTTP/3 measurement failed: %w", err)
		}

		printComparisonResults(url, http12Trace, http3Trace, http12Sample.TTFB, http3Sample.TTFB)

		return nil
	}

	// Multi-sample comparison mode
	var http12Samples []stats.AssetSample

	var http3Samples []stats.AssetSample

	// Collect HTTP/1.1-2 samples
	if verbose {
		fmt.Println("\n══ HTTP/1.1-2 Samples ══")
	}

	for i := 0; i < samples; i++ {
		if verbose {
			fmt.Printf("\n── Sample %d/%d ──\n", i+1, samples)
		}

		sample, _, err := measureTTFB()
		if err != nil {
			return fmt.Errorf("HTTP/1.1-2 sample %d failed: %w", i+1, err)
		}

		http12Samples = append(http12Samples, sample)

		if verbose {
			fmt.Printf("  TTFB: %s\n", formatDuration(sample.TTFB))
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
		fmt.Println("\n══ HTTP/3 Samples ══")
	}

	for i := 0; i < samples; i++ {
		if verbose {
			fmt.Printf("\n── Sample %d/%d ──\n", i+1, samples)
		}

		sample, _, err := measureTTFBHTTP3()
		if err != nil {
			return fmt.Errorf("HTTP/3 sample %d failed: %w", i+1, err)
		}

		http3Samples = append(http3Samples, sample)

		if verbose {
			fmt.Printf("  TTFB: %s\n", formatDuration(sample.TTFB))
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

	printMultiSampleComparisonResults(url, http12Samples, http3Samples)

	return nil
}

// measureTTFBHTTP3 performs a single TTFB measurement using HTTP/3
func measureTTFBHTTP3() (stats.AssetSample, *probe.Trace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTP3Client(timeout)

	if verbose {
		fmt.Printf("Fetching asset (HTTP/3): %s\n", url)
	}

	resp, trace, err := probe.FetchWithTraceHTTP3(ctx, url, client)
	if err != nil {
		return stats.AssetSample{}, nil, fmt.Errorf("failed to fetch asset: %w", err)
	}

	defer resp.Body.Close()

	// Drain body to complete the request
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return stats.AssetSample{}, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	sample := stats.AssetSample{
		DNSLookup:     trace.DNSLookup,
		QUICHandshake: trace.QUICHandshake,
		TTFB:          trace.TTFB,
		TotalTime:     trace.Total,
	}

	return sample, trace, nil
}

// measureTTFB performs a single TTFB measurement
func measureTTFB() (stats.AssetSample, *probe.Trace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := probe.NewHTTPClient(timeout)

	if verbose {
		fmt.Printf("Fetching asset: %s\n", url)
	}

	resp, trace, err := probe.FetchWithTrace(ctx, url, client)
	if err != nil {
		return stats.AssetSample{}, nil, fmt.Errorf("failed to fetch asset: %w", err)
	}

	defer resp.Body.Close()

	// Drain body to complete the request
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return stats.AssetSample{}, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	sample := stats.AssetSample{
		DNSLookup:    trace.DNSLookup,
		TCPConnect:   trace.TCPConnect,
		TLSHandshake: trace.TLSHandshake,
		TTFB:         trace.TTFB,
		TotalTime:    trace.Total,
	}

	return sample, trace, nil
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
func printResults(url string, trace *probe.Trace, ttfb time.Duration) {
	fmt.Printf("atrace results for: %s\n", url)
	fmt.Println("────────────────────────────────────────────────────")
	fmt.Printf("DNS Lookup:                  %12s\n", formatDuration(trace.DNSLookup))
	fmt.Printf("TCP Connect:                 %12s\n", formatDuration(trace.TCPConnect))
	fmt.Printf("TLS Handshake:               %12s\n", formatDuration(trace.TLSHandshake))
	fmt.Println("────────────────────────────────────────────────────")
	fmt.Printf("Total TTFB:                  %12s\n", formatDuration(ttfb))
}

// printMultiSampleResults outputs aggregate statistics for multiple samples
func printMultiSampleResults(url string, allSamples []stats.AssetSample) {
	ttfbDurations := stats.ExtractAssetTTFB(allSamples)
	outliers := stats.DetectOutliers(ttfbDurations)

	// Determine which durations to use for stats
	durationsForStats := ttfbDurations

	if excludeOutliers && len(outliers) > 0 {
		durationsForStats = stats.ExcludeOutliers(ttfbDurations, outliers)
	}

	avgLabel := "Avg"

	if excludeOutliers && len(outliers) > 0 {
		avgLabel = "Avg*"
	}

	fmt.Printf("\natrace results for: %s (%d samples)\n", url, len(allSamples))
	fmt.Println("──────────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %12s %12s %12s %12s %12s\n", "", avgLabel, "Min", "Max", "Median", "StdDev")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────────")

	printStatRow("DNS Lookup:", stats.ExtractAssetDNSLookup(allSamples), outliers)
	printStatRow("TCP Connect:", stats.ExtractAssetTCPConnect(allSamples), outliers)
	printStatRow("TLS Handshake:", stats.ExtractAssetTLSHandshake(allSamples), outliers)

	fmt.Println("──────────────────────────────────────────────────────────────────────────────────")

	ttfbStats := stats.ComputeStats(durationsForStats)

	fmt.Printf("%-20s %12s %12s %12s %12s %12s\n",
		"Total TTFB:",
		formatDuration(ttfbStats.Mean),
		formatDuration(ttfbStats.Min),
		formatDuration(ttfbStats.Max),
		formatDuration(ttfbStats.Median),
		formatDuration(ttfbStats.StdDev),
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

// printComparisonResults outputs side-by-side HTTP/1.1-2 vs HTTP/3 comparison
func printComparisonResults(url string, http12Trace, http3Trace *probe.Trace, http12TTFB, http3TTFB time.Duration) {
	fmt.Printf("atrace comparison for: %s\n", url)
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
		"Total TTFB:",
		formatDuration(http12TTFB),
		formatDuration(http3TTFB),
		formatDelta(http12TTFB, http3TTFB),
	)
}

// printMultiSampleComparisonResults outputs aggregate stats for HTTP/1.1-2 vs HTTP/3
func printMultiSampleComparisonResults(url string, http12Samples, http3Samples []stats.AssetSample) {
	fmt.Printf("\natrace comparison for: %s (%d samples each)\n", url, len(http12Samples))
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s %14s %14s %14s\n", "", "HTTP/1.1-2", "HTTP/3", "Delta")
	fmt.Println("────────────────────────────────────────────────────────────────────")

	// DNS Lookup
	http12DNS := stats.ComputeStats(stats.ExtractAssetDNSLookup(http12Samples))
	http3DNS := stats.ComputeStats(stats.ExtractAssetDNSLookup(http3Samples))

	fmt.Printf("%-20s %14s %14s %14s\n",
		"DNS Lookup:",
		formatDuration(http12DNS.Mean),
		formatDuration(http3DNS.Mean),
		formatDelta(http12DNS.Mean, http3DNS.Mean),
	)

	// TCP Connect (HTTP/1.1-2 only)
	http12TCP := stats.ComputeStats(stats.ExtractAssetTCPConnect(http12Samples))

	fmt.Printf("%-20s %14s %14s %14s\n",
		"TCP Connect:",
		formatDuration(http12TCP.Mean),
		"N/A",
		"N/A",
	)

	// TLS Handshake (HTTP/1.1-2 only)
	http12TLS := stats.ComputeStats(stats.ExtractAssetTLSHandshake(http12Samples))

	fmt.Printf("%-20s %14s %14s %14s\n",
		"TLS Handshake:",
		formatDuration(http12TLS.Mean),
		"N/A",
		"N/A",
	)

	// QUIC Handshake (HTTP/3 only)
	http3QUIC := stats.ComputeStats(stats.ExtractAssetQUICHandshake(http3Samples))

	fmt.Printf("%-20s %14s %14s %14s\n",
		"QUIC Handshake:",
		"N/A",
		formatDuration(http3QUIC.Mean),
		"N/A",
	)

	fmt.Println("────────────────────────────────────────────────────────────────────")

	// Total TTFB
	http12TTFB := stats.ComputeStats(stats.ExtractAssetTTFB(http12Samples))
	http3TTFB := stats.ComputeStats(stats.ExtractAssetTTFB(http3Samples))

	fmt.Printf("%-20s %14s %14s %14s\n",
		"Total TTFB:",
		formatDuration(http12TTFB.Mean),
		formatDuration(http3TTFB.Mean),
		formatDelta(http12TTFB.Mean, http3TTFB.Mean),
	)
}
