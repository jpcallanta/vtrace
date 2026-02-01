package probe

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// Trace holds timing metrics captured during an HTTP request
type Trace struct {
	DNSLookup     time.Duration
	TCPConnect    time.Duration
	TLSHandshake  time.Duration
	QUICHandshake time.Duration
	TTFB          time.Duration
	Total         time.Duration
}

// traceState holds intermediate timestamps during request tracing
type traceState struct {
	start             time.Time
	dnsStart          time.Time
	dnsDone           time.Time
	connectStart      time.Time
	connectDone       time.Time
	tlsHandshakeStart time.Time
	tlsHandshakeDone  time.Time
	firstByte         time.Time
}

// FetchWithTrace performs an HTTP GET request and returns timing metrics
func FetchWithTrace(ctx context.Context, url string, client *http.Client) (*http.Response, *Trace, error) {
	state := &traceState{}

	clientTrace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			state.dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			state.dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			state.connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			state.connectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			state.tlsHandshakeStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			state.tlsHandshakeDone = time.Now()
		},
		GotFirstResponseByte: func() {
			state.firstByte = time.Now()
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), clientTrace))

	state.start = time.Now()

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	trace := buildTrace(state)

	return resp, trace, nil
}

// buildTrace calculates durations from captured timestamps
func buildTrace(state *traceState) *Trace {
	trace := &Trace{}

	// Calculate DNS lookup duration if DNS occurred
	if !state.dnsStart.IsZero() && !state.dnsDone.IsZero() {
		trace.DNSLookup = state.dnsDone.Sub(state.dnsStart)
	}

	// Calculate TCP connect duration
	if !state.connectStart.IsZero() && !state.connectDone.IsZero() {
		trace.TCPConnect = state.connectDone.Sub(state.connectStart)
	}

	// Calculate TLS handshake duration
	if !state.tlsHandshakeStart.IsZero() && !state.tlsHandshakeDone.IsZero() {
		trace.TLSHandshake = state.tlsHandshakeDone.Sub(state.tlsHandshakeStart)
	}

	// Calculate time to first byte from request start
	if !state.firstByte.IsZero() {
		trace.TTFB = state.firstByte.Sub(state.start)
	}

	// Calculate total duration
	trace.Total = time.Since(state.start)

	return trace
}

// NewHTTPClient creates an HTTP client with the specified timeout
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
	}
}

// NewHTTP3Client creates an HTTP/3 client with the specified timeout
func NewHTTP3Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{},
		},
	}
}

// http3TraceState holds intermediate timestamps during HTTP/3 request tracing
type http3TraceState struct {
	start     time.Time
	dnsStart  time.Time
	dnsDone   time.Time
	gotConn   time.Time
	firstByte time.Time
}

// FetchWithTraceHTTP3 performs an HTTP/3 GET request and returns timing metrics
func FetchWithTraceHTTP3(ctx context.Context, url string, client *http.Client) (*http.Response, *Trace, error) {
	state := &http3TraceState{}

	clientTrace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			state.dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			state.dnsDone = time.Now()
		},
		GotConn: func(_ httptrace.GotConnInfo) {
			state.gotConn = time.Now()
		},
		GotFirstResponseByte: func() {
			state.firstByte = time.Now()
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), clientTrace))

	state.start = time.Now()

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	trace := buildHTTP3Trace(state)

	return resp, trace, nil
}

// buildHTTP3Trace calculates durations from captured HTTP/3 timestamps
func buildHTTP3Trace(state *http3TraceState) *Trace {
	trace := &Trace{}

	// Calculate DNS lookup duration if DNS occurred
	if !state.dnsStart.IsZero() && !state.dnsDone.IsZero() {
		trace.DNSLookup = state.dnsDone.Sub(state.dnsStart)
	}

	// Calculate QUIC handshake as time from after DNS to connection ready
	quicStart := state.start

	if !state.dnsDone.IsZero() {
		quicStart = state.dnsDone
	}

	if !state.gotConn.IsZero() {
		trace.QUICHandshake = state.gotConn.Sub(quicStart)
	}

	// Calculate time to first byte from request start
	if !state.firstByte.IsZero() {
		trace.TTFB = state.firstByte.Sub(state.start)
	}

	// Calculate total duration
	trace.Total = time.Since(state.start)

	return trace
}
