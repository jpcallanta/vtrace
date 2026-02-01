package stats

import (
	"math"
	"sort"
	"time"
)

// Sample holds timing data from a single TTFF measurement
type Sample struct {
	DNSLookup      time.Duration
	TCPConnect     time.Duration
	TLSHandshake   time.Duration
	QUICHandshake  time.Duration
	ManifestTTFB   time.Duration
	ManifestTotal  time.Duration
	SegmentTotal   time.Duration
	FrameDetection time.Duration
	TotalTTFF      time.Duration
}

// Outlier represents a sample identified as an outlier
type Outlier struct {
	Index     int
	Value     time.Duration
	Deviation float64
}

// Stats holds computed statistics for a set of duration samples
type Stats struct {
	Mean   time.Duration
	Median time.Duration
	Min    time.Duration
	Max    time.Duration
	StdDev time.Duration
}

// ComputeStats calculates statistics for a slice of durations
func ComputeStats(durations []time.Duration) Stats {
	if len(durations) == 0 {
		return Stats{}
	}

	if len(durations) == 1 {
		return Stats{
			Mean:   durations[0],
			Median: durations[0],
			Min:    durations[0],
			Max:    durations[0],
			StdDev: 0,
		}
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	min := sorted[0]
	max := sorted[len(sorted)-1]
	median := computeMedian(sorted)
	mean := computeMean(durations)
	stdDev := computeStdDev(durations, mean)

	return Stats{
		Mean:   mean,
		Median: median,
		Min:    min,
		Max:    max,
		StdDev: stdDev,
	}
}

// computeMean calculates the arithmetic mean of durations
func computeMean(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var sum time.Duration

	for _, d := range durations {
		sum += d
	}

	return sum / time.Duration(len(durations))
}

// computeMedian calculates the median of a sorted slice of durations
func computeMedian(sorted []time.Duration) time.Duration {
	n := len(sorted)

	if n == 0 {
		return 0
	}

	if n%2 == 1 {
		return sorted[n/2]
	}

	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// computeStdDev calculates the standard deviation of durations
func computeStdDev(durations []time.Duration, mean time.Duration) time.Duration {
	if len(durations) < 2 {
		return 0
	}

	var sumSquares float64

	meanFloat := float64(mean)

	for _, d := range durations {
		diff := float64(d) - meanFloat
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(len(durations)-1)

	return time.Duration(math.Sqrt(variance))
}

// DetectOutliers identifies outliers using the IQR method
func DetectOutliers(durations []time.Duration) []Outlier {
	if len(durations) < 4 {
		return nil
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	q1 := computeQuartile(sorted, 0.25)
	q3 := computeQuartile(sorted, 0.75)
	iqr := q3 - q1

	lowerBound := q1 - time.Duration(float64(iqr)*1.5)
	upperBound := q3 + time.Duration(float64(iqr)*1.5)

	mean := computeMean(durations)

	var outliers []Outlier

	for i, d := range durations {
		if d < lowerBound || d > upperBound {
			deviation := (float64(d) - float64(mean)) / float64(mean) * 100

			outliers = append(outliers, Outlier{
				Index:     i,
				Value:     d,
				Deviation: deviation,
			})
		}
	}

	return outliers
}

// computeQuartile calculates the quartile value at the given percentile
func computeQuartile(sorted []time.Duration, percentile float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	index := percentile * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	weight := index - float64(lower)

	return time.Duration(float64(sorted[lower])*(1-weight) + float64(sorted[upper])*weight)
}

// ExcludeOutliers returns a new slice with outlier values removed
func ExcludeOutliers(durations []time.Duration, outliers []Outlier) []time.Duration {
	if len(outliers) == 0 {
		return durations
	}

	outlierIndices := make(map[int]bool)

	for _, o := range outliers {
		outlierIndices[o.Index] = true
	}

	var filtered []time.Duration

	for i, d := range durations {
		if !outlierIndices[i] {
			filtered = append(filtered, d)
		}
	}

	return filtered
}

// ExtractTotalTTFF extracts TotalTTFF from a slice of samples
func ExtractTotalTTFF(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TotalTTFF
	}

	return durations
}

// ExtractDNSLookup extracts DNSLookup from a slice of samples
func ExtractDNSLookup(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.DNSLookup
	}

	return durations
}

// ExtractTCPConnect extracts TCPConnect from a slice of samples
func ExtractTCPConnect(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TCPConnect
	}

	return durations
}

// ExtractTLSHandshake extracts TLSHandshake from a slice of samples
func ExtractTLSHandshake(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TLSHandshake
	}

	return durations
}

// ExtractQUICHandshake extracts QUICHandshake from a slice of samples
func ExtractQUICHandshake(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.QUICHandshake
	}

	return durations
}

// ExtractManifestTTFB extracts ManifestTTFB from a slice of samples
func ExtractManifestTTFB(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.ManifestTTFB
	}

	return durations
}

// ExtractSegmentTotal extracts SegmentTotal from a slice of samples
func ExtractSegmentTotal(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.SegmentTotal
	}

	return durations
}

// ExtractFrameDetection extracts FrameDetection from a slice of samples
func ExtractFrameDetection(samples []Sample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.FrameDetection
	}

	return durations
}

// AssetSample holds timing data from a single TTFB measurement for any asset
type AssetSample struct {
	DNSLookup     time.Duration
	TCPConnect    time.Duration
	TLSHandshake  time.Duration
	QUICHandshake time.Duration
	TTFB          time.Duration
	TotalTime     time.Duration
}

// ExtractAssetTTFB extracts TTFB from a slice of asset samples
func ExtractAssetTTFB(samples []AssetSample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TTFB
	}

	return durations
}

// ExtractAssetDNSLookup extracts DNSLookup from a slice of asset samples
func ExtractAssetDNSLookup(samples []AssetSample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.DNSLookup
	}

	return durations
}

// ExtractAssetTCPConnect extracts TCPConnect from a slice of asset samples
func ExtractAssetTCPConnect(samples []AssetSample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TCPConnect
	}

	return durations
}

// ExtractAssetTLSHandshake extracts TLSHandshake from a slice of asset samples
func ExtractAssetTLSHandshake(samples []AssetSample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TLSHandshake
	}

	return durations
}

// ExtractAssetTotalTime extracts TotalTime from a slice of asset samples
func ExtractAssetTotalTime(samples []AssetSample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.TotalTime
	}

	return durations
}

// ExtractAssetQUICHandshake extracts QUICHandshake from a slice of asset samples
func ExtractAssetQUICHandshake(samples []AssetSample) []time.Duration {
	durations := make([]time.Duration, len(samples))

	for i, s := range samples {
		durations[i] = s.QUICHandshake
	}

	return durations
}
