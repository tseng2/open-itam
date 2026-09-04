package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type ProbeFuncs struct {
	BaseboardSerial func() (string, error)
	FirstMAC        func() (string, error)
}

func DeviceID(p ProbeFuncs) (string, error) {
	serial, _ := p.BaseboardSerial()
	mac, _ := p.FirstMAC()
	serial = strings.TrimSpace(serial)
	if isGarbageSerial(serial) {
		serial = ""
	}
	mac = normalizeMAC(mac)
	if serial == "" && mac == "" {
		return "", fmt.Errorf("cannot derive device id: no usable hardware identifier")
	}
	sum := sha256.Sum256([]byte(serial + "|" + mac))
	return hex.EncodeToString(sum[:])[:16], nil
}

func normalizeMAC(mac string) string {
	mac = strings.TrimSpace(mac)
	if mac == "" {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(mac, "-", ":"))
}

var garbageSerials = []string{
	"default string",
	"to be filled by o.e.m.",
	"system serial number",
	"none",
	"not specified",
	"unknown",
	"0",
}

func isGarbageSerial(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, g := range garbageSerials {
		if lower == g {
			return true
		}
	}
	return false
}
