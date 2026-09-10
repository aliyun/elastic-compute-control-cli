//go:build !windows

package e2bapi

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func lookupSystemCNAME(ctx context.Context, host string) (map[string]string, error) {
	servers, err := systemDNSServers(ctx, host)
	if err != nil {
		return nil, err
	}
	return lookupCNAMEAtServers(ctx, host, servers)
}

func lookupCNAMEAtServers(ctx context.Context, host string, servers []string) (map[string]string, error) {
	var lastErr error = errors.New("no system DNS servers")
	for _, server := range servers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		answer, err := queryCNAME(attempt, host, server)
		cancel()
		if err == nil {
			return answer, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func queryCNAME(ctx context.Context, host, server string) (map[string]string, error) {
	name, err := dnsmessage.NewName(normalizeDNSName(host) + ".")
	if err != nil {
		return nil, err
	}
	var random [2]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	id := binary.BigEndian.Uint16(random[:])
	question := dnsmessage.Question{Name: name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}
	request, err := (&dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{question},
	}).Pack()
	if err != nil {
		return nil, err
	}
	response, err := exchangeDNS(ctx, "udp", server, request)
	if err != nil {
		return nil, err
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(response)
	if err != nil {
		return nil, err
	}
	if header.ID != id || !header.Response || header.OpCode != 0 {
		return nil, errors.New("invalid DNS response header")
	}
	if header.Truncated {
		response, err = exchangeDNS(ctx, "tcp", server, request)
		if err != nil {
			return nil, err
		}
	}
	var message dnsmessage.Message
	if err := message.Unpack(response); err != nil {
		return nil, err
	}
	if message.ID != id || !message.Response || message.OpCode != 0 || message.Truncated || message.RCode != dnsmessage.RCodeSuccess {
		return nil, errors.New("unsuccessful DNS response")
	}
	if len(message.Questions) != 1 || normalizeDNSName(message.Questions[0].Name.String()) != normalizeDNSName(host) || message.Questions[0].Type != question.Type || message.Questions[0].Class != question.Class {
		return nil, errors.New("mismatched DNS question")
	}
	answer := map[string]string{}
	for _, record := range message.Answers {
		cname, ok := record.Body.(*dnsmessage.CNAMEResource)
		if !ok || record.Header.Class != dnsmessage.ClassINET {
			continue
		}
		owner, target := normalizeDNSName(record.Header.Name.String()), normalizeDNSName(cname.CNAME.String())
		if previous, ok := answer[owner]; ok && previous != target {
			return nil, errors.New("conflicting CNAME records")
		}
		answer[owner] = target
	}
	return answer, nil
}

func exchangeDNS(ctx context.Context, network, server string, request []byte) ([]byte, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}
	if network == "tcp" {
		packet := make([]byte, 2, len(request)+2)
		binary.BigEndian.PutUint16(packet, uint16(len(request)))
		packet = append(packet, request...)
		if _, err := conn.Write(packet); err != nil {
			return nil, err
		}
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return nil, err
		}
		response := make([]byte, binary.BigEndian.Uint16(size[:]))
		_, err = io.ReadFull(conn, response)
		return response, err
	}
	if _, err := conn.Write(request); err != nil {
		return nil, err
	}
	response := make([]byte, 65535)
	n, err := conn.Read(response)
	return response[:n], err
}
