package copilot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func FuzzFrameLength(f *testing.F) {
	for _, header := range []string{
		"Content-Length: 2\r\n\r\n{}",
		"content-length: 2\nX-Extra: value\n\n{}",
		"Content-Length: 2\r\nContent-Length: 3\r\n\r\n{}",
		"Content-Length: -1\r\n\r\n",
		"Content-Length: 9999999999999999999999\r\n\r\n",
		"Content-Length: 65537\r\n\r\n",
		strings.Repeat("X", maxProtocolHeaderBytes+1),
		"", "\r\n", "Content-Length: 2\r\n",
	} {
		f.Add([]byte(header))
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		source := bytes.NewReader(wire)
		reader := bufio.NewReaderSize(source, maxProtocolHeaderBytes)
		length, err := frameLength(reader, maxConnectBytes)
		if err != nil {
			return
		}
		if length <= 0 || length > maxConnectBytes {
			t.Fatalf("accepted out-of-bounds length: %d", length)
		}
		consumed := len(wire) - source.Len() - reader.Buffered()
		if consumed <= 0 || consumed > maxProtocolHeaderBytes {
			t.Fatalf("accepted out-of-bounds header: %d bytes", consumed)
		}
	})
}

func FuzzGuardedFrames(f *testing.F) {
	f.Add([]byte("hello"), uint16(0))
	f.Add([]byte("hello"), uint16(1))
	f.Add([]byte{0, '\n', '\r', 255}, uint16(4))
	f.Fuzz(func(t *testing.T, payload []byte, missing uint16) {
		if len(payload) == 0 || len(payload) > maxConnectBytes {
			return
		}
		// Use valid JSON-RPC objects; the guarded transport now also inspects
		// notification metadata to reserve the event's bytes before forwarding.
		body, err := json.Marshal(map[string]string{"result": string(payload)})
		if err != nil {
			t.Fatal(err)
		}
		first := "Content-Length: 2\r\n\r\n{}"
		wire := append([]byte(first+fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
		omitted := int(missing) % (len(body) + 1)
		wire = wire[:len(wire)-omitted]
		var output bytes.Buffer
		err = copyFrames(&output, bufio.NewReaderSize(bytes.NewReader(wire), maxProtocolHeaderBytes), newEventBudget(maxQueuedEventBytes))
		wantOutput := wire
		if omitted != 0 {
			wantOutput = []byte(first) // Partial payloads are never forwarded.
		}
		if !bytes.Equal(output.Bytes(), wantOutput) {
			t.Fatal("framing changed a complete forwarded body or frame boundary")
		}
		want := io.EOF
		if omitted != 0 {
			want = io.ErrUnexpectedEOF
		}
		if !errors.Is(err, want) {
			t.Fatalf("incorrect truncation result: got %v, want %v", err, want)
		}
	})
}

func FuzzBridgeAuthentication(f *testing.F) {
	const token = "fuzz-bridge-token"
	for _, body := range []string{
		`{"id":7,"method":"connect","params":{"token":"fuzz-bridge-token","extra":{"value":[1,true,null]}}}`,
		`{"method":"connect","params":{"token":"wrong-token"}}`,
		`{"method":"ping","params":{"token":"fuzz-bridge-token"}}`,
		`{"method":"connect","params":null}`, `null`, `[]`, `invalid-json`,
		strings.Repeat(" ", maxConnectBytes+1),
	} {
		f.Add([]byte(body))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > maxConnectBytes+1 {
			return
		}
		decode := func(data []byte) (map[string]any, error) {
			var object map[string]any
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber() // Preserve arbitrary valid JSON numbers in extra fields.
			if err := decoder.Decode(&object); err != nil {
				return nil, err
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				return nil, errors.New("trailing JSON data")
			}
			return object, nil
		}
		original, parseErr := decode(body)
		params, _ := original["params"].(map[string]any)
		wantAccepted := parseErr == nil && len(body) <= maxConnectBytes && original["method"] == "connect" && params["token"] == token
		wire := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
		forwarded, err := authenticate(bufio.NewReaderSize(io.MultiReader(strings.NewReader(wire), bytes.NewReader(body)), maxProtocolHeaderBytes), token)
		if (err == nil) != wantAccepted {
			t.Fatalf("authentication decision changed: accepted=%t expected=%t error=%v", err == nil, wantAccepted, err)
		}
		if err != nil {
			return
		}
		delete(params, "token")
		got, err := decode(forwarded)
		if err != nil || !reflect.DeepEqual(got, original) {
			t.Fatalf("forwarded request changed beyond removing the bridge token: %v", err)
		}
	})
}
