package dns

import (
	"context"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"artesian/internal/config"
)

// Interceptor handles DNS query matching and resolution
type Interceptor struct {
	cfg           config.DNSConfig
	interceptIP   net.IP
	upstreamAddrs []string
	patterns      []string
	mu            sync.RWMutex
}

// normalizeUpstreams ensures all upstream DNS targets have an explicit port (:53 by default)
func normalizeUpstreams(addrs []string) []string {
	var cleaned []string
	for _, a := range addrs {
		s := strings.TrimSpace(a)
		if s == "" {
			continue
		}
		if !strings.Contains(s, ":") {
			s += ":53"
		}
		cleaned = append(cleaned, s)
	}
	return cleaned
}

// NewInterceptor initializes a DNS interceptor with specified rules
func NewInterceptor(cfg config.DNSConfig, patterns []string) *Interceptor {
	ip := net.ParseIP(cfg.InterceptIPv4)
	if ip == nil {
		ip = net.ParseIP("127.0.0.1")
	}

	return &Interceptor{
		cfg:           cfg,
		interceptIP:   ip,
		upstreamAddrs: normalizeUpstreams(cfg.UpstreamDNS),
		patterns:      patterns,
	}
}

// GetInterceptIP returns the current target IP thread-safely
func (i *Interceptor) GetInterceptIP() net.IP {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.interceptIP
}

// GetUpstreamAddrs returns the current upstream resolvers thread-safely
func (i *Interceptor) GetUpstreamAddrs() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]string, len(i.upstreamAddrs))
	copy(out, i.upstreamAddrs)
	return out
}

// UpdateConfig dynamically updates the intercept IP and upstream resolvers
func (i *Interceptor) UpdateConfig(interceptIP string, upstreams []string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if interceptIP != "" {
		if ip := net.ParseIP(interceptIP); ip != nil {
			i.interceptIP = ip
			i.cfg.InterceptIPv4 = interceptIP
			log.Printf("[Artesian DNS] Target Intercept IP updated to %s", ip)
		}
	}
	cleanedUpstreams := normalizeUpstreams(upstreams)
	if len(cleanedUpstreams) > 0 {
		i.upstreamAddrs = cleanedUpstreams
		i.cfg.UpstreamDNS = cleanedUpstreams
		log.Printf("[Artesian DNS] Upstream DNS resolvers updated to %v", cleanedUpstreams)
	}
}

// Matches returns true if the queried domain belongs to an intercepted game service
func (i *Interceptor) Matches(domain string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	cleaned := strings.TrimSuffix(strings.ToLower(domain), ".")
	for _, pattern := range i.patterns {
		pattern = strings.ToLower(pattern)
		if pattern == cleaned {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(cleaned, suffix) || cleaned == strings.TrimPrefix(pattern, "*.") {
				return true
			}
		} else if strings.Contains(pattern, "*") {
			if matched, _ := filepath.Match(pattern, cleaned); matched {
				return true
			}
		}
	}
	return false
}

// Server provides UDP DNS listening and upstream proxy forwarding
type Server struct {
	interceptor *Interceptor
	listenAddr  string
	conn        *net.UDPConn
	shutdown    chan struct{}
}

// NewServer creates a new DNS server
func NewServer(cfg config.DNSConfig, patterns []string) *Server {
	return &Server{
		interceptor: NewInterceptor(cfg, patterns),
		listenAddr:  cfg.ListenAddr,
		shutdown:    make(chan struct{}),
	}
}

// Start launches the UDP DNS listener
func (s *Server) Start(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("invalid DNS listen address %s: %w", s.listenAddr, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to bind DNS UDP socket on %s: %w", s.listenAddr, err)
	}
	s.conn = conn

	log.Printf("[Artesian DNS] Server active on %s (Upstreams: %v, Intercept Target: %s)",
		s.listenAddr, s.interceptor.upstreamAddrs, s.interceptor.interceptIP)

	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.shutdown:
			return nil
		default:
		}

		_ = s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, clientAddr, err := s.conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-s.shutdown:
				return nil
			default:
				log.Printf("[Artesian DNS] Read error: %v", err)
				continue
			}
		}

		reqPacket := make([]byte, n)
		copy(reqPacket, buffer[:n])

		go s.handleQuery(clientAddr, reqPacket)
	}
}

// UpdateConfig dynamically updates intercept IP and upstream resolvers on the running server
func (s *Server) UpdateConfig(interceptIP string, upstreams []string) {
	if s != nil && s.interceptor != nil {
		s.interceptor.UpdateConfig(interceptIP, upstreams)
	}
}

// handleQuery processes or forwards the incoming DNS query packet
func (s *Server) handleQuery(clientAddr net.Addr, packet []byte) {
	domain, isQuery := extractQuestionDomain(packet)
	if isQuery && s.interceptor.Matches(domain) {
		targetIP := s.interceptor.GetInterceptIP()
		log.Printf("[Artesian DNS -> Intercepted] %s queried %s -> Redirecting to %s",
			clientAddr.String(), domain, targetIP)

		resp := buildSyntheticAResponse(packet, targetIP)
		if len(resp) > 0 {
			_, _ = s.conn.WriteTo(resp, clientAddr)
			return
		}
	}

	if isQuery {
		dLower := strings.ToLower(domain)
		if strings.Contains(dLower, "steam") {
			log.Printf("[Artesian DNS -> Forwarded] %s queried %s (not in intercept list)", clientAddr.String(), domain)
		}
	}

	// Forward transparently to upstream DNS resolvers
	s.forwardUpstream(clientAddr, packet)
}

func (s *Server) forwardUpstream(clientAddr net.Addr, packet []byte) {
	upstreams := s.interceptor.GetUpstreamAddrs()
	for _, upstream := range upstreams {
		upAddr, err := net.ResolveUDPAddr("udp", upstream)
		if err != nil {
			continue
		}

		upConn, err := net.DialUDP("udp", nil, upAddr)
		if err != nil {
			continue
		}

		_ = upConn.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = upConn.Write(packet)
		if err != nil {
			_ = upConn.Close()
			continue
		}

		respBuf := make([]byte, 4096)
		n, err := upConn.Read(respBuf)
		_ = upConn.Close()
		if err != nil {
			continue
		}

		_, _ = s.conn.WriteTo(respBuf[:n], clientAddr)
		return
	}
}

// Stop terminates the DNS server
func (s *Server) Stop() {
	close(s.shutdown)
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

// extractQuestionDomain performs lightweight parsing of the standard DNS query domain name
func extractQuestionDomain(packet []byte) (string, bool) {
	if len(packet) < 12 {
		return "", false
	}
	// Check header QR flag (bit 7 of byte 2): 0 is query
	if (packet[2] & 0x80) != 0 {
		return "", false
	}

	offset := 12
	var labels []string
	for offset < len(packet) {
		length := int(packet[offset])
		offset++
		if length == 0 {
			break
		}
		if offset+length > len(packet) {
			return "", false
		}
		labels = append(labels, string(packet[offset:offset+length]))
		offset += length
	}

	if len(labels) == 0 {
		return "", false
	}
	return strings.Join(labels, "."), true
}

// buildSyntheticAResponse generates an answer packet returning the proxy IP
func buildSyntheticAResponse(query []byte, ip net.IP) []byte {
	if len(query) < 12 {
		return nil
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil
	}

	// Find the end of the question section (QNAME + QTYPE(2) + QCLASS(2))
	offset := 12
	for offset < len(query) {
		length := int(query[offset])
		offset++
		if length == 0 {
			break
		}
		offset += length
	}
	questionEnd := offset + 4
	if questionEnd > len(query) {
		return nil
	}

	resp := make([]byte, questionEnd)
	copy(resp, query[:questionEnd])

	// Modify Header:
	// Set QR=1 (response), AA=1 (authoritative), RA=1 (recursion available)
	resp[2] = 0x81
	resp[3] = 0x80
	// ANCOUNT = 1
	resp[6] = 0x00
	resp[7] = 0x01
	// NSCOUNT = 0, ARCOUNT = 0 (clear authority and any query EDNS0 options)
	resp[8] = 0x00
	resp[9] = 0x00
	resp[10] = 0x00
	resp[11] = 0x00

	// Answer Resource Record:
	// Pointer to question name (0xc00c)
	answer := []byte{
		0xc0, 0x0c, // Pointer to domain in question
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
		0x00, 0x00, 0x00, 0x3c, // TTL: 60 seconds
		0x00, 0x04, // Data Length: 4 bytes
		ipv4[0], ipv4[1], ipv4[2], ipv4[3], // Resolved proxy IP
	}

	return append(resp, answer...)
}
