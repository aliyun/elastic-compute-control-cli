//go:build !windows

package e2bapi

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestCNAMEWireLookup(t *testing.T) {
	for _, mode := range []string{"udp", "tcp fallback", "wrong ID", "wrong question", "NXDOMAIN", "additional only", "conflicting records"} {
		t.Run(mode, func(t *testing.T) {
			server := testDNSServer(t, func(network string, query dnsmessage.Message) dnsmessage.Message {
				response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions}
				name, _ := dnsmessage.NewName("api.custom.example.")
				target, _ := dnsmessage.NewName("api.cn-beijing.e2b.fc.aliyuncs.com.")
				response.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}, Body: &dnsmessage.CNAMEResource{CNAME: target}}}
				switch mode {
				case "tcp fallback":
					response.Truncated = network == "udp"
				case "wrong ID":
					response.ID++
				case "wrong question":
					response.Questions = nil
				case "NXDOMAIN":
					response.RCode = dnsmessage.RCodeNameError
				case "additional only":
					response.Additionals, response.Answers = response.Answers, nil
				case "conflicting records":
					other, _ := dnsmessage.NewName("other.example.")
					response.Answers = append(response.Answers, dnsmessage.Resource{Header: response.Answers[0].Header, Body: &dnsmessage.CNAMEResource{CNAME: other}})
				}
				return response
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			answer, err := queryCNAME(ctx, "api.custom.example", server)
			switch mode {
			case "udp", "tcp fallback":
				if err != nil || !reflect.DeepEqual(answer, map[string]string{"api.custom.example": "api.cn-beijing.e2b.fc.aliyuncs.com"}) {
					t.Fatalf("answer=%v err=%v", answer, err)
				}
			case "additional only":
				if err != nil || len(answer) != 0 {
					t.Fatalf("additional section trusted: %v err=%v", answer, err)
				}
			default:
				if err == nil {
					t.Fatalf("accepted invalid response: %v", answer)
				}
			}
		})
	}
}

func TestCNAMEWireLookupUsesNextSystemServer(t *testing.T) {
	bad := testDNSServer(t, func(_ string, query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RCode: dnsmessage.RCodeServerFailure}, Questions: query.Questions}
	})
	good := testDNSServer(t, func(_ string, query dnsmessage.Message) dnsmessage.Message {
		return dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := lookupCNAMEAtServers(ctx, "api.custom.example", []string{bad, good}); err != nil {
		t.Fatal(err)
	}
}

func testDNSServer(t *testing.T, respond func(string, dnsmessage.Message) dnsmessage.Message) string {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tcp.Close() })
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	pack := func(network string, data []byte) []byte {
		var query dnsmessage.Message
		if err := query.Unpack(data); err != nil {
			t.Error(err)
			return nil
		}
		if len(query.Questions) != 1 || query.Questions[0].Type != dnsmessage.TypeCNAME || !query.RecursionDesired {
			t.Error("invalid CNAME query")
		}
		response := respond(network, query)
		encoded, err := response.Pack()
		if err != nil {
			t.Error(err)
		}
		return encoded
	}
	go func() {
		buffer := make([]byte, 65535)
		n, addr, err := udp.ReadFrom(buffer)
		if err != nil {
			return
		}
		_, _ = udp.WriteTo(pack("udp", buffer[:n]), addr)
	}()
	go func() {
		conn, err := tcp.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return
		}
		buffer := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(conn, buffer); err != nil {
			return
		}
		response := pack("tcp", buffer)
		binary.BigEndian.PutUint16(size[:], uint16(len(response)))
		_, _ = conn.Write(append(size[:], response...))
	}()
	return tcp.Addr().String()
}
