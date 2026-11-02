//go:build !windows

package e2bapi

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSLookupRejectsMalformedPacketsAndUnavailableFallback(t *testing.T) {
	for _, mode := range []string{"short header", "short question", "TCP unavailable"} {
		t.Run(mode, func(t *testing.T) {
			server, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			go func() {
				request := make([]byte, 4096)
				n, peer, err := server.ReadFrom(request)
				if err != nil {
					return
				}
				var query dnsmessage.Message
				if err := query.Unpack(request[:n]); err != nil {
					return
				}
				response := []byte{0}
				if mode != "short header" {
					message := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, Truncated: mode == "TCP unavailable"}, Questions: query.Questions}
					response, _ = message.Pack()
					if mode == "short question" {
						response = response[:12]
					}
				}
				_, _ = server.WriteTo(response, peer)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := queryCNAME(ctx, "api.example", server.LocalAddr().String()); err == nil {
				t.Fatalf("%s response accepted", mode)
			}
		})
	}
	if _, err := queryCNAME(context.Background(), string(make([]byte, 300)), "127.0.0.1:1"); err == nil {
		t.Fatal("invalid DNS name accepted")
	}
	if _, err := exchangeDNS(context.Background(), "tcp", "invalid-address", nil); err == nil {
		t.Fatal("invalid DNS server accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lookupCNAMEAtServers(ctx, "api.example", []string{"127.0.0.1:1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DNS cancellation lost: %v", err)
	}
	if _, err := lookupCNAMEAtServers(context.Background(), "api.example", nil); err == nil {
		t.Fatal("missing DNS servers accepted")
	}
}

func TestDNSIgnoresNonCNAMEAnswersAndShortTCPFrames(t *testing.T) {
	server := testDNSServer(t, func(_ string, query dnsmessage.Message) dnsmessage.Message {
		name, _ := dnsmessage.NewName("api.example.")
		target, _ := dnsmessage.NewName("alias.example.")
		return dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions, Answers: []dnsmessage.Resource{
			{Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}},
			{Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassCHAOS}, Body: &dnsmessage.CNAMEResource{CNAME: target}},
		}}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if answer, err := queryCNAME(ctx, "api.example", server); err != nil || len(answer) != 0 {
		t.Fatalf("untrusted answer retained: %v,%v", answer, err)
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		conn, err := tcp.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = conn.Write([]byte{0})
		}
	}()
	if _, err := exchangeDNS(ctx, "tcp", tcp.Addr().String(), nil); err == nil {
		t.Fatal("short TCP DNS frame accepted")
	}
}
