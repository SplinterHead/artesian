package steam

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Steam EMsg constants
const (
	EMsgMulti                       = 1
	EMsgServiceMethodResponse       = 147
	EMsgServiceMethodCallFromClient = 151
	EMsgClientLogOnResponse         = 751
	EMsgClientLogon                 = 5514
	ProtoMask                       = 0x80000000
)

// ProtoHeader represents the decoded fields of CMsgProtoBufHeader
type ProtoHeader struct {
	SteamID       uint64
	ClientSession int32
	RoutingAppID  uint32
	JobIDSource   uint64
	JobIDTarget   uint64
	TargetJobName string
	EResult       int32
}

// CMPacket represents a single unpacked Steam CM packet
type CMPacket struct {
	EMsg    uint32
	IsProto bool
	Header  ProtoHeader
	Body    []byte
}

// CMClient provides native pure-Go communication with Steam Connection Managers
type CMClient struct {
	conn            *websocket.Conn
	packetQueue     []CMPacket
	queueMu         sync.Mutex
	steamID         uint64
	clientSessionID int32
	nextJobID       uint64
	closed          bool
	closeMu         sync.Mutex
}

// NewCMClient connects to an active Steam CM and logs in using genuine session credentials
func NewCMClient(ctx context.Context, username, refreshToken string, steamID uint64) (*CMClient, error) {
	cmEndpoints := resolveCMEndpoints()
	var lastErr error
	var conn *websocket.Conn

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 8 * time.Second,
	}

	for _, endpoint := range cmEndpoints {
		wsURL := fmt.Sprintf("wss://%s/cmsocket/", endpoint)
		headers := http.Header{"User-Agent": {"Valve/Steam HTTP Client 1.0"}}

		c, _, err := dialer.DialContext(ctx, wsURL, headers)
		if err == nil {
			conn = c
			break
		}
		lastErr = err
	}

	if conn == nil {
		return nil, fmt.Errorf("failed to connect to any Steam CM: %w", lastErr)
	}

	client := &CMClient{
		conn:      conn,
		steamID:   steamID,
		nextJobID: 100,
	}

	// Authenticate with genuine Steam session
	if err := client.logon(ctx, username, refreshToken); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("steam logon failed: %w", err)
	}

	return client, nil
}

// Close closes the underlying CM WebSocket connection
func (c *CMClient) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// logon performs initial authentication over Steam CM
func (c *CMClient) logon(ctx context.Context, username, refreshToken string) error {
	jobID := atomic.AddUint64(&c.nextJobID, 1)

	// Build CMsgProtoBufHeader
	var hdr []byte
	hdr = append(hdr, encodeFixed64(1, c.steamID)...)
	hdr = append(hdr, encodeUint32(2, 0)...)            // client_sessionid = 0
	hdr = append(hdr, encodeFixed64(10, jobID)...)     // jobid_source

	// Build CMsgClientLogon body
	var body []byte
	body = append(body, encodeUint32(1, 65580)...)              // protocol_version
	body = append(body, encodeString(50, username)...)          // account_name
	body = append(body, encodeString(108, refreshToken)...)     // access_token (JWT refresh token)
	body = append(body, encodeUint32(6, 1)...)                  // should_remember_password = 1

	packet := buildPacket(EMsgClientLogon, hdr, body)
	if err := c.conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		return fmt.Errorf("failed to write logon packet: %w", err)
	}

	// Read packets until EMsgClientLogOnResponse (751)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pkt, err := c.readNextPacket(5 * time.Second)
		if err != nil {
			return err
		}

		if pkt.EMsg == EMsgClientLogOnResponse {
			eresult := pkt.Header.EResult
			var sessionID int32

			// Parse response body fields
			offset := 0
			for offset < len(pkt.Body) {
				tag, off, err := readVarint(pkt.Body, offset)
				if err != nil {
					break
				}
				offset = off
				wire := int(tag & 7)
				field := int(tag >> 3)

				if wire == 0 && field == 1 { // eresult
					res, off, _ := readVarint(pkt.Body, offset)
					eresult = int32(res)
					offset = off
				} else if wire == 0 && field == 14 { // client_sessionid
					sess, off, _ := readVarint(pkt.Body, offset)
					sessionID = int32(sess)
					offset = off
				} else {
					offset, _ = skipField(wire, pkt.Body, offset)
				}
			}

			if eresult != 1 {
				return fmt.Errorf("steam CM rejected logon with EResult %d", eresult)
			}

			c.clientSessionID = sessionID
			if c.clientSessionID == 0 {
				c.clientSessionID = pkt.Header.ClientSession
			}
			return nil
		}
	}

	return fmt.Errorf("timed out waiting for Steam CM logon response")
}

// GetManifestRequestCode requests the genuine rotating manifest request code for a depot
func (c *CMClient) GetManifestRequestCode(ctx context.Context, appID, depotID uint32, manifestID uint64, branch string) (uint64, error) {
	if branch == "" {
		branch = "public"
	}

	jobID := atomic.AddUint64(&c.nextJobID, 1)

	// Build CMsgProtoBufHeader
	var hdr []byte
	hdr = append(hdr, encodeFixed64(1, c.steamID)...)
	hdr = append(hdr, encodeUint32(2, uint32(c.clientSessionID))...)
	hdr = append(hdr, encodeFixed64(10, jobID)...) // jobid_source
	hdr = append(hdr, encodeString(12, "ContentServerDirectory.GetManifestRequestCode#1")...)

	// Build CContentServerDirectory_GetManifestRequestCode_Request body
	var body []byte
	body = append(body, encodeUint32(1, appID)...)
	body = append(body, encodeUint32(2, depotID)...)
	body = append(body, encodeUint64(3, manifestID)...)
	body = append(body, encodeString(4, branch)...)

	packet := buildPacket(EMsgServiceMethodCallFromClient, hdr, body)
	if err := c.conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		return 0, fmt.Errorf("failed to send GetManifestRequestCode RPC: %w", err)
	}

	// Read packets until response matching our jobID arrives
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		pkt, err := c.readNextPacket(5 * time.Second)
		if err != nil {
			return 0, err
		}

		if (pkt.EMsg == EMsgServiceMethodResponse || pkt.Header.JobIDTarget == jobID) && pkt.Header.JobIDTarget == jobID {
			if pkt.Header.EResult != 1 && pkt.Header.EResult != 0 {
				return 0, fmt.Errorf("GetManifestRequestCode failed with EResult %d (access denied or invalid branch)", pkt.Header.EResult)
			}

			// Parse CContentServerDirectory_GetManifestRequestCode_Response (tag 1: manifest_request_code)
			var requestCode uint64
			offset := 0
			for offset < len(pkt.Body) {
				tag, off, err := readVarint(pkt.Body, offset)
				if err != nil {
					break
				}
				offset = off
				wire := int(tag & 7)
				field := int(tag >> 3)

				if wire == 0 && field == 1 {
					val, off, err := readVarint(pkt.Body, offset)
					if err == nil {
						requestCode = val
					}
					offset = off
				} else {
					offset, err = skipField(wire, pkt.Body, offset)
					if err != nil {
						break
					}
				}
			}

			if requestCode == 0 {
				return 0, fmt.Errorf("Steam CM returned zero manifest request code")
			}
			return requestCode, nil
		}
	}

	return 0, fmt.Errorf("timed out waiting for GetManifestRequestCode response")
}

// readNextPacket pulls the next packet from queue or reads from WebSocket, unpacking EMsgMulti
func (c *CMClient) readNextPacket(timeout time.Duration) (CMPacket, error) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	for len(c.packetQueue) == 0 {
		c.conn.SetReadDeadline(time.Now().Add(timeout))
		_, rawMsg, err := c.conn.ReadMessage()
		if err != nil {
			return CMPacket{}, err
		}

		pkt, err := parseCMPacket(rawMsg)
		if err != nil {
			continue
		}

		if pkt.EMsg == EMsgMulti {
			subs, err := unpackMulti(pkt.Body)
			if err != nil {
				continue
			}
			for _, sub := range subs {
				if sp, err := parseCMPacket(sub); err == nil {
					c.packetQueue = append(c.packetQueue, sp)
				}
			}
		} else {
			c.packetQueue = append(c.packetQueue, pkt)
		}
	}

	pkt := c.packetQueue[0]
	c.packetQueue = c.packetQueue[1:]
	return pkt, nil
}

// resolveCMEndpoints returns optimal Steam CM WebSocket endpoints
func resolveCMEndpoints() []string {
	// Standard high-reliability fallback list
	defaults := []string{
		"cmp1-lhr1.steamserver.net:443",
		"cmp2-lhr1.steamserver.net:443",
		"cmp1-fra1.steamserver.net:443",
		"cmp2-ams1.steamserver.net:27018",
		"cmp1-ord1.steamserver.net:443",
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.steampowered.com/ISteamDirectory/GetCMListForConnect/v1/?cellid=0")
	if err != nil {
		return defaults
	}
	defer resp.Body.Close()

	var data struct {
		Response struct {
			ServerList []struct {
				Endpoint string `json:"endpoint"`
				Type     string `json:"type"`
			} `json:"serverlist_websockets"`
		} `json:"response"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && len(data.Response.ServerList) > 0 {
		var list []string
		for _, s := range data.Response.ServerList {
			if s.Endpoint != "" {
				list = append(list, s.Endpoint)
			}
		}
		if len(list) > 0 {
			return list
		}
	}

	return defaults
}

// Protobuf helper utilities
func encodeVarint(val uint64) []byte {
	var buf []byte
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			buf = append(buf, b|0x80)
		} else {
			buf = append(buf, b)
			break
		}
	}
	return buf
}

func encodeTag(fieldNum int, wireType int) []byte {
	return encodeVarint(uint64(fieldNum<<3 | wireType))
}

func encodeFixed64(fieldNum int, val uint64) []byte {
	buf := encodeTag(fieldNum, 1)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], val)
	return append(buf, b[:]...)
}

func encodeUint32(fieldNum int, val uint32) []byte {
	buf := encodeTag(fieldNum, 0)
	return append(buf, encodeVarint(uint64(val))...)
}

func encodeUint64(fieldNum int, val uint64) []byte {
	buf := encodeTag(fieldNum, 0)
	return append(buf, encodeVarint(val)...)
}

func encodeString(fieldNum int, val string) []byte {
	data := []byte(val)
	buf := encodeTag(fieldNum, 2)
	buf = append(buf, encodeVarint(uint64(len(data)))...)
	return append(buf, data...)
}

func readVarint(data []byte, offset int) (uint64, int, error) {
	var res uint64
	var shift uint
	for offset < len(data) {
		b := data[offset]
		offset++
		res |= uint64(b&0x7f) << shift
		if (b & 0x80) == 0 {
			return res, offset, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, offset, fmt.Errorf("varint overflow")
		}
	}
	return 0, offset, fmt.Errorf("unexpected EOF")
}

func skipField(wire int, data []byte, offset int) (int, error) {
	switch wire {
	case 0:
		_, off, err := readVarint(data, offset)
		return off, err
	case 1:
		if offset+8 > len(data) {
			return offset, fmt.Errorf("EOF fixed64")
		}
		return offset + 8, nil
	case 2:
		length, off, err := readVarint(data, offset)
		if err != nil {
			return off, err
		}
		if off+int(length) > len(data) {
			return off, fmt.Errorf("EOF length delimited")
		}
		return off + int(length), nil
	case 5:
		if offset+4 > len(data) {
			return offset, fmt.Errorf("EOF fixed32")
		}
		return offset + 4, nil
	default:
		return offset, fmt.Errorf("unknown wire type %d", wire)
	}
}

func decodeHeader(data []byte) (ProtoHeader, error) {
	var h ProtoHeader
	offset := 0
	for offset < len(data) {
		tag, off, err := readVarint(data, offset)
		if err != nil {
			return h, err
		}
		offset = off
		wire := int(tag & 7)
		field := int(tag >> 3)

		switch field {
		case 1: // steamid fixed64
			if offset+8 > len(data) {
				return h, fmt.Errorf("EOF steamid")
			}
			h.SteamID = binary.LittleEndian.Uint64(data[offset : offset+8])
			offset += 8
		case 2: // client_sessionid int32
			val, off, err := readVarint(data, offset)
			if err != nil {
				return h, err
			}
			h.ClientSession = int32(val)
			offset = off
		case 10: // jobid_source fixed64
			if offset+8 > len(data) {
				return h, fmt.Errorf("EOF jobid_source")
			}
			h.JobIDSource = binary.LittleEndian.Uint64(data[offset : offset+8])
			offset += 8
		case 11: // jobid_target fixed64
			if offset+8 > len(data) {
				return h, fmt.Errorf("EOF jobid_target")
			}
			h.JobIDTarget = binary.LittleEndian.Uint64(data[offset : offset+8])
			offset += 8
		case 12: // target_job_name string
			length, off, err := readVarint(data, offset)
			if err != nil {
				return h, err
			}
			if off+int(length) > len(data) {
				return h, fmt.Errorf("EOF target_job_name")
			}
			h.TargetJobName = string(data[off : off+int(length)])
			offset = off + int(length)
		case 13: // eresult int32
			val, off, err := readVarint(data, offset)
			if err != nil {
				return h, err
			}
			h.EResult = int32(val)
			offset = off
		default:
			off, err := skipField(wire, data, offset)
			if err != nil {
				return h, err
			}
			offset = off
		}
	}
	return h, nil
}

func buildPacket(eMsg uint32, headerBytes []byte, bodyBytes []byte) []byte {
	rawEMsg := eMsg | ProtoMask
	hdrLen := uint32(len(headerBytes))

	packet := make([]byte, 8+len(headerBytes)+len(bodyBytes))
	binary.LittleEndian.PutUint32(packet[0:4], rawEMsg)
	binary.LittleEndian.PutUint32(packet[4:8], hdrLen)
	copy(packet[8:8+len(headerBytes)], headerBytes)
	copy(packet[8+len(headerBytes):], bodyBytes)
	return packet
}

func parseCMPacket(msg []byte) (CMPacket, error) {
	if len(msg) < 8 {
		return CMPacket{}, fmt.Errorf("packet too short")
	}
	rawEMsg := binary.LittleEndian.Uint32(msg[0:4])
	isProto := (rawEMsg & ProtoMask) != 0
	eMsg := rawEMsg & ^uint32(ProtoMask)

	if !isProto {
		var body []byte
		if len(msg) > 8 {
			body = msg[8:]
		}
		return CMPacket{
			EMsg:    eMsg,
			IsProto: false,
			Body:    body,
		}, nil
	}

	hdrLen := binary.LittleEndian.Uint32(msg[4:8])
	if int(8+hdrLen) > len(msg) {
		return CMPacket{}, fmt.Errorf("corrupt packet: hdrLen %d > len %d", hdrLen, len(msg))
	}

	protoHdr, _ := decodeHeader(msg[8 : 8+hdrLen])
	return CMPacket{
		EMsg:    eMsg,
		IsProto: true,
		Header:  protoHdr,
		Body:    msg[8+hdrLen:],
	}, nil
}

func unpackMulti(body []byte) ([][]byte, error) {
	var sizeUnzipped uint32
	var messageBody []byte

	offset := 0
	for offset < len(body) {
		tag, off, err := readVarint(body, offset)
		if err != nil {
			break
		}
		offset = off
		wire := int(tag & 7)
		field := int(tag >> 3)

		if wire == 0 && field == 1 {
			val, off, err := readVarint(body, offset)
			if err != nil {
				return nil, err
			}
			sizeUnzipped = uint32(val)
			offset = off
		} else if wire == 2 && field == 2 {
			length, off, err := readVarint(body, offset)
			if err != nil {
				return nil, err
			}
			if off+int(length) > len(body) {
				return nil, fmt.Errorf("EOF message_body")
			}
			messageBody = body[off : off+int(length)]
			offset = off + int(length)
		} else {
			off, err := skipField(wire, body, offset)
			if err != nil {
				return nil, err
			}
			offset = off
		}
	}

	var payload []byte
	if sizeUnzipped > 0 {
		gz, err := gzip.NewReader(bytes.NewReader(messageBody))
		if err != nil {
			return nil, fmt.Errorf("gzip error: %w", err)
		}
		defer gz.Close()
		payload, err = io.ReadAll(gz)
		if err != nil {
			return nil, fmt.Errorf("gzip read error: %w", err)
		}
	} else {
		payload = messageBody
	}

	var subPackets [][]byte
	pOff := 0
	for pOff+4 <= len(payload) {
		subLen := int(binary.LittleEndian.Uint32(payload[pOff : pOff+4]))
		pOff += 4
		if pOff+subLen > len(payload) {
			break
		}
		subPackets = append(subPackets, payload[pOff:pOff+subLen])
		pOff += subLen
	}
	return subPackets, nil
}
