package slimproto

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestServerFrameRoundTripViaBuffer(t *testing.T) {
	// Build a server frame manually: [len BE u16][op 4 bytes][payload]
	payload := []byte("hello-payload")
	total := 4 + len(payload)
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint16(total))
	buf.Write([]byte{'s', 't', 'r', 'm'})
	buf.Write(payload)

	frame, err := ReadServerFrame(buf)
	if err != nil {
		t.Fatalf("ReadServerFrame: %v", err)
	}
	if frame.Op != OpStrm {
		t.Errorf("op: got %q, want strm", string(frame.Op[:]))
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Errorf("payload: got %q, want %q", frame.Payload, payload)
	}
}

func TestReadServerFrameRejectsTooShortLength(t *testing.T) {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint16(2)) // < 4
	buf.Write([]byte{'x', 'x'})

	if _, err := ReadServerFrame(buf); err == nil {
		t.Fatal("expected error for length < 4")
	}
}

func TestWriteClientFrame(t *testing.T) {
	buf := new(bytes.Buffer)
	if err := WriteClientFrame(buf, OpHELO, []byte{1, 2, 3}); err != nil {
		t.Fatalf("WriteClientFrame: %v", err)
	}
	got := buf.Bytes()
	// [H E L O][00 00 00 03][01 02 03]
	want := []byte{'H', 'E', 'L', 'O', 0, 0, 0, 3, 1, 2, 3}
	if !bytes.Equal(got, want) {
		t.Errorf("frame bytes: got %v, want %v", got, want)
	}
}

func TestEncodeHELO(t *testing.T) {
	h := HelloInfo{
		DeviceID:     12,
		Revision:     1,
		MAC:          [6]byte{0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
		Language:     [2]byte{'e', 'n'},
		Capabilities: "Model=direttampd,flc",
	}
	out := EncodeHELO(h)
	// 1 + 1 + 6 + 16 + 2 + 8 + 2 = 36 bytes of fixed header + caps
	if len(out) != 36+len(h.Capabilities) {
		t.Fatalf("HELO size: got %d, want %d", len(out), 36+len(h.Capabilities))
	}
	if out[0] != 12 || out[1] != 1 {
		t.Errorf("device/revision: got %d/%d", out[0], out[1])
	}
	if !bytes.Equal(out[2:8], h.MAC[:]) {
		t.Errorf("MAC mismatch")
	}
	if !bytes.HasSuffix(out, []byte(h.Capabilities)) {
		t.Errorf("capabilities suffix missing")
	}
}

func TestEncodeSTATSize(t *testing.T) {
	out := EncodeSTAT(StatInfo{Event: StatSTMt})
	if len(out) != 53 {
		t.Errorf("STAT payload size: got %d, want 53", len(out))
	}
	if !bytes.HasPrefix(out, []byte{'S', 'T', 'M', 't'}) {
		t.Errorf("STAT event prefix: got %q", out[:4])
	}
}

func TestDecodeStrmHeaderAndPath(t *testing.T) {
	// Build a minimal strm payload: 24-byte header + HTTP request line.
	httpReq := "GET /stream.mp3?player=ab%3Acd HTTP/1.0\r\n\r\n"
	header := make([]byte, 24)
	header[0] = 's'                         // command
	header[1] = '1'                         // autostart
	header[2] = 'm'                         // format: mp3
	binary.BigEndian.PutUint16(header[18:20], 9000) // server port
	payload := append(header, []byte(httpReq)...)

	cmd, err := DecodeStrm(payload)
	if err != nil {
		t.Fatalf("DecodeStrm: %v", err)
	}
	if cmd.Command != 's' || cmd.FormatCode != 'm' || cmd.ServerPort != 9000 {
		t.Errorf("fields: %+v", cmd)
	}
	if got := cmd.HTTPRequestPath(); got != "/stream.mp3?player=ab%3Acd" {
		t.Errorf("path: got %q", got)
	}
}

func TestDecodeStrmShortPayload(t *testing.T) {
	if _, err := DecodeStrm(make([]byte, 10)); err == nil {
		t.Fatal("expected error for short payload")
	}
}

func TestParseOrDeriveMACExplicit(t *testing.T) {
	mac, err := parseOrDeriveMAC("00:11:22:33:44:55")
	if err != nil {
		t.Fatalf("parseOrDeriveMAC: %v", err)
	}
	want := [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	if mac != want {
		t.Errorf("MAC: got %v, want %v", mac, want)
	}
}

func TestParseOrDeriveMACInvalid(t *testing.T) {
	for _, in := range []string{"", "xx:yy", "00:11:22:33:44", "00:11:22:33:44:55:66", "00:11:22:33:44:zz"} {
		if in == "" {
			continue // empty triggers hostname-derived MAC path, tested separately
		}
		if _, err := parseOrDeriveMAC(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestParseOrDeriveMACFromHostname(t *testing.T) {
	mac, err := parseOrDeriveMAC("")
	if err != nil {
		t.Fatalf("parseOrDeriveMAC(\"\"): %v", err)
	}
	// Locally-administered bit must be set, multicast bit cleared.
	if mac[0]&0x02 == 0 {
		t.Errorf("local-admin bit not set: %02x", mac[0])
	}
	if mac[0]&0x01 != 0 {
		t.Errorf("multicast bit set: %02x", mac[0])
	}
}

func TestFormatExtension(t *testing.T) {
	cases := map[byte]string{
		'f': ".flac",
		'm': ".mp3",
		'a': ".aac",
		'o': ".ogg",
		'p': ".pcm",
		'?': "",
	}
	for in, want := range cases {
		if got := formatExtension(in); got != want {
			t.Errorf("formatExtension(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestHTTPRequestPathFallback(t *testing.T) {
	cmd := StrmCmd{HTTPRequest: []byte("GET\r\n")}
	if got := cmd.HTTPRequestPath(); got != "/" {
		t.Errorf("fallback path: got %q, want /", got)
	}
	cmd = StrmCmd{}
	if got := cmd.HTTPRequestPath(); got != "/" {
		t.Errorf("empty path: got %q, want /", got)
	}
}

func TestEncodeHELOCapabilitiesASCII(t *testing.T) {
	h := HelloInfo{Capabilities: "Model=x,flc"}
	out := EncodeHELO(h)
	tail := string(out[len(out)-len(h.Capabilities):])
	if !strings.HasPrefix(tail, "Model=") {
		t.Errorf("capabilities not at tail: %q", tail)
	}
}
