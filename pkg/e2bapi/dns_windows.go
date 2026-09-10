package e2bapi

import (
	"context"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DnsQuery uses the Windows resolver, including split DNS policies. Its API
// cannot be cancelled; bound outstanding calls and let their records be freed
// even if the caller's overall detection deadline has already expired.
var windowsDNSCalls = make(chan struct{}, 4)

func lookupSystemCNAME(ctx context.Context, host string) (map[string]string, error) {
	select {
	case windowsDNSCalls <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	type result struct {
		records map[string]string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-windowsDNSCalls }()
		records, err := queryWindowsCNAME(host)
		done <- result{records, err}
	}()
	select {
	case r := <-done:
		return r.records, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func queryWindowsCNAME(host string) (map[string]string, error) {
	var head *windows.DNSRecord
	err := windows.DnsQuery(normalizeDNSName(host)+".", windows.DNS_TYPE_CNAME, 0, nil, &head, nil)
	if head != nil {
		defer windows.DnsRecordListFree(head, 1)
	}
	if errors.Is(err, windows.DNS_INFO_NO_RECORDS) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	answer := map[string]string{}
	for record := head; record != nil; record = record.Next {
		if record.Type != windows.DNS_TYPE_CNAME || record.Dw&3 != 1 {
			continue
		}
		data := (*windows.DNSPTRData)(unsafe.Pointer(&record.Data[0]))
		owner := normalizeDNSName(windows.UTF16PtrToString(record.Name))
		target := normalizeDNSName(windows.UTF16PtrToString(data.Host))
		if previous, ok := answer[owner]; ok && previous != target {
			return nil, errors.New("conflicting CNAME records")
		}
		answer[owner] = target
	}
	return answer, nil
}
