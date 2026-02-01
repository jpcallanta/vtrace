package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafov/m3u8"
)

var (
	ErrNoVariants      = errors.New("master playlist has no variants")
	ErrNoSegments      = errors.New("media playlist has no segments")
	ErrInvalidPlaylist = errors.New("invalid or unrecognized playlist format")
)

// PlaylistResult holds the parsed playlist and associated trace data
type PlaylistResult struct {
	Master *m3u8.MasterPlaylist
	Media  *m3u8.MediaPlaylist
	Trace  *Trace
}

// FetchPlaylist fetches and parses an HLS playlist from the given URL
func FetchPlaylist(ctx context.Context, hlsURL string, client *http.Client) (*PlaylistResult, error) {
	resp, trace, err := FetchWithTrace(ctx, hlsURL, client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playlist fetch returned status %d", resp.StatusCode)
	}

	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse playlist: %w", err)
	}

	result := &PlaylistResult{Trace: trace}

	switch listType {
	case m3u8.MASTER:
		result.Master = playlist.(*m3u8.MasterPlaylist)
	case m3u8.MEDIA:
		result.Media = playlist.(*m3u8.MediaPlaylist)
	default:
		return nil, ErrInvalidPlaylist
	}

	return result, nil
}

// FetchPlaylistHTTP3 fetches and parses an HLS playlist using HTTP/3
func FetchPlaylistHTTP3(ctx context.Context, hlsURL string, client *http.Client) (*PlaylistResult, error) {
	resp, trace, err := FetchWithTraceHTTP3(ctx, hlsURL, client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("playlist fetch returned status %d", resp.StatusCode)
	}

	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse playlist: %w", err)
	}

	result := &PlaylistResult{Trace: trace}

	switch listType {
	case m3u8.MASTER:
		result.Master = playlist.(*m3u8.MasterPlaylist)
	case m3u8.MEDIA:
		result.Media = playlist.(*m3u8.MediaPlaylist)
	default:
		return nil, ErrInvalidPlaylist
	}

	return result, nil
}

// GetFirstVariantURL extracts the URL of the first variant from a master playlist
func GetFirstVariantURL(master *m3u8.MasterPlaylist, baseURL string) (string, error) {
	if master == nil || len(master.Variants) == 0 {
		return "", ErrNoVariants
	}

	variantURI := master.Variants[0].URI

	return resolveURL(baseURL, variantURI)
}

// GetFirstSegmentURL extracts the URL of the first segment from a media playlist
func GetFirstSegmentURL(media *m3u8.MediaPlaylist, baseURL string) (string, error) {
	if media == nil {
		return "", ErrNoSegments
	}

	// Find the first non-nil segment
	for _, seg := range media.Segments {
		if seg != nil && seg.URI != "" {
			return resolveURL(baseURL, seg.URI)
		}
	}

	return "", ErrNoSegments
}

// DownloadSegment downloads a segment and returns the body as bytes
func DownloadSegment(ctx context.Context, segmentURL string, client *http.Client) ([]byte, *Trace, error) {
	resp, trace, err := FetchWithTrace(ctx, segmentURL, client)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download segment: %w", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("segment download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read segment data: %w", err)
	}

	return data, trace, nil
}

// DownloadSegmentHTTP3 downloads a segment using HTTP/3 and returns the body as bytes
func DownloadSegmentHTTP3(ctx context.Context, segmentURL string, client *http.Client) ([]byte, *Trace, error) {
	resp, trace, err := FetchWithTraceHTTP3(ctx, segmentURL, client)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download segment: %w", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("segment download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read segment data: %w", err)
	}

	return data, trace, nil
}

// resolveURL resolves a potentially relative URL against a base URL
func resolveURL(baseURL, ref string) (string, error) {
	// Check if ref is already absolute
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	refURL, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("failed to parse reference URL: %w", err)
	}

	resolved := base.ResolveReference(refURL)

	return resolved.String(), nil
}

// GetBaseURL extracts the base URL from a full URL (removes the filename)
func GetBaseURL(fullURL string) (string, error) {
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Remove the last path component (the filename)
	lastSlash := strings.LastIndex(parsed.Path, "/")
	if lastSlash >= 0 {
		parsed.Path = parsed.Path[:lastSlash+1]
	}

	return parsed.String(), nil
}
