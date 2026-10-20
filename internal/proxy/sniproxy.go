package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// SNIProxy provides transparent TCP passthrough for TLS/HTTPS traffic (port 443).
// When DNS interceptor redirects CDN domains to Artesian, clients often request
// manifest or auth endpoints over HTTPS. SNIProxy inspects the TLS ClientHello SNI header
// and tunnels the encrypted stream to the genuine upstream destination without breaking TLS.
type SNIProxy struct {
	listenAddr   string
	upstreamDNS  string
	listener     net.Listener
	shutdown     chan struct{}
	wg           sync.WaitGroup
	dialer       *net.Dialer
	mu           sync.RWMutex
}

// NewSNIProxy initializes an SNI pass-through proxy
func NewSNIProxy(listenAddr, upstreamDNS string) *SNIProxy {
	if listenAddr == "" {
		listenAddr = ":443"
	}
	p := &SNIProxy{
		listenAddr:  listenAddr,
		upstreamDNS: upstreamDNS,
		shutdown:    make(chan struct{}),
	}
	p.updateDialer(upstreamDNS)
	return p
}

// SetUpstreamDNS updates the upstream DNS resolver used by the SNI proxy
func (p *SNIProxy) SetUpstreamDNS(upstream string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.upstreamDNS = upstream
	p.updateDialer(upstream)
}

func (p *SNIProxy) updateDialer(upstream string) {
	target := strings.TrimSpace(upstream)
	if target == "" {
		target = "1.1.1.1:53"
	}
	if !strings.Contains(target, ":") {
		target += ":53"
	}

	p.dialer = &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(ctx, "udp", target)
			},
		},
	}
}

// Start launches the TCP listener for TLS pass-through
func (p *SNIProxy) Start() error {
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to bind SNI proxy on %s: %w", p.listenAddr, err)
	}
	p.listener = ln
	log.Printf("[Artesian SNI Proxy] HTTPS transparent SNI pass-through online on %s", p.listenAddr)

	p.wg.Add(1)
	go p.acceptLoop()
	return nil
}

func (p *SNIProxy) acceptLoop() {
	defer p.wg.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.shutdown:
				return
			default:
				log.Printf("[Artesian SNI Proxy] Accept error: %v", err)
				continue
			}
		}

		p.wg.Add(1)
		go func(c net.Conn) {
			defer p.wg.Done()
			p.handleConn(c)
		}(conn)
	}
}

func (p *SNIProxy) handleConn(clientConn net.Conn) {
	defer clientConn.Close()

	// Read initial ClientHello with deadline
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 16384)
	n, err := clientConn.Read(buf)
	if err != nil {
		return
	}

	sni, err := ExtractSNI(buf[:n])
	if err != nil || sni == "" {
		log.Printf("[Artesian SNI Proxy] Could not extract SNI from %s: %v", clientConn.RemoteAddr(), err)
		return
	}

	p.mu.RLock()
	dialer := p.dialer
	p.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	upstreamAddr := net.JoinHostPort(sni, "443")
	upstreamConn, err := dialer.DialContext(ctx, "tcp", upstreamAddr)
	if err != nil {
		log.Printf("[Artesian SNI Proxy] Failed to dial upstream %s for client %s: %v", upstreamAddr, clientConn.RemoteAddr(), err)
		return
	}
	defer upstreamConn.Close()

	// Clear deadlines for bidirectional data piping
	_ = clientConn.SetDeadline(time.Time{})
	_ = upstreamConn.SetDeadline(time.Time{})

	// Replay initial ClientHello bytes to upstream
	if _, err := upstreamConn.Write(buf[:n]); err != nil {
		return
	}

	log.Printf("[Artesian SNI Proxy] Tunneling HTTPS TLS connection from %s -> %s", clientConn.RemoteAddr(), upstreamAddr)

	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}

	go pipe(upstreamConn, clientConn)
	go pipe(clientConn, upstreamConn)

	<-done
}

// Stop gracefully shuts down the SNI proxy
func (p *SNIProxy) Stop() error {
	select {
	case <-p.shutdown:
		return nil
	default:
		close(p.shutdown)
	}
	if p.listener != nil {
		_ = p.listener.Close()
	}
	p.wg.Wait()
	return nil
}

// ExtractSNI parses the Server Name Indication from a raw TLS ClientHello byte buffer
func ExtractSNI(data []byte) (string, error) {
	if len(data) < 5 {
		return "", errors.New("data too short for TLS record")
	}

	// TLS record header: ContentType (1 byte, 0x16 = Handshake)
	if data[0] != 0x16 {
		return "", errors.New("not a TLS handshake record")
	}

	recordLen := int(binary.BigEndian.Uint16(data[3:5]))
	if len(data) < 5+recordLen {
		// Even if buffer was truncated by MTU, parse what we have if long enough
		recordLen = len(data) - 5
	}

	handshake := data[5 : 5+recordLen]
	if len(handshake) < 4 {
		return "", errors.New("handshake too short")
	}

	// HandshakeType 0x01 = ClientHello
	if handshake[0] != 0x01 {
		return "", errors.New("not a ClientHello message")
	}

	// Skip handshake header (4 bytes: type 1 + length 3)
	pos := 4
	// Client version (2 bytes) + Random (32 bytes) = 34 bytes
	pos += 34
	if pos > len(handshake) {
		return "", errors.New("truncated after random")
	}

	// Session ID: 1 byte len + bytes
	if pos >= len(handshake) {
		return "", errors.New("truncated at session id")
	}
	sessionIDLen := int(handshake[pos])
	pos += 1 + sessionIDLen
	if pos > len(handshake) {
		return "", errors.New("truncated after session id")
	}

	// Cipher suites: 2 bytes len + bytes
	if pos+2 > len(handshake) {
		return "", errors.New("truncated at cipher suites")
	}
	cipherSuitesLen := int(binary.BigEndian.Uint16(handshake[pos : pos+2]))
	pos += 2 + cipherSuitesLen
	if pos > len(handshake) {
		return "", errors.New("truncated after cipher suites")
	}

	// Compression methods: 1 byte len + bytes
	if pos >= len(handshake) {
		return "", errors.New("truncated at compression methods")
	}
	compressionLen := int(handshake[pos])
	pos += 1 + compressionLen
	if pos > len(handshake) {
		return "", errors.New("truncated after compression methods")
	}

	// Extensions: 2 bytes len + bytes
	if pos+2 > len(handshake) {
		return "", errors.New("no extensions present")
	}
	extensionsLen := int(binary.BigEndian.Uint16(handshake[pos : pos+2]))
	pos += 2
	extensionsEnd := pos + extensionsLen
	if extensionsEnd > len(handshake) {
		extensionsEnd = len(handshake)
	}

	for pos+4 <= extensionsEnd {
		extType := binary.BigEndian.Uint16(handshake[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(handshake[pos+2 : pos+4]))
		pos += 4

		if pos+extLen > extensionsEnd {
			break
		}

		// Extension 0x0000 = server_name (SNI)
		if extType == 0x0000 {
			sniData := handshake[pos : pos+extLen]
			if len(sniData) < 2 {
				return "", errors.New("malformed sni extension")
			}
			listLen := int(binary.BigEndian.Uint16(sniData[0:2]))
			sniPos := 2
			for sniPos+3 <= 2+listLen && sniPos+3 <= len(sniData) {
				nameType := sniData[sniPos]
				nameLen := int(binary.BigEndian.Uint16(sniData[sniPos+1 : sniPos+3]))
				sniPos += 3
				if sniPos+nameLen > len(sniData) {
					break
				}
				if nameType == 0x00 { // 0x00 = host_name
					return strings.ToLower(string(sniData[sniPos : sniPos+nameLen])), nil
				}
				sniPos += nameLen
			}
		}

		pos += extLen
	}

	return "", errors.New("sni extension not found in ClientHello")
}
