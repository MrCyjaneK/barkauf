package main

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestFixedLnurlSats(t *testing.T) {
	sats, ok := fixedLnurlSats(lnurlPay{minMsat: 1500000, maxMsat: 1500000})
	if !ok || sats != 1500 {
		t.Fatalf("fixed amount %d %v", sats, ok)
	}
	if _, ok := fixedLnurlSats(lnurlPay{minMsat: 1000, maxMsat: 2000}); ok {
		t.Fatal("a range is not a fixed amount")
	}
	if _, ok := fixedLnurlSats(lnurlPay{minMsat: 1500, maxMsat: 1500}); ok {
		t.Fatal("a fractional sat amount is not filled")
	}
	if _, ok := fixedLnurlSats(lnurlPay{}); ok {
		t.Fatal("an empty range is not a fixed amount")
	}
}

func TestBech32BIP173(t *testing.T) {
	hrp, data, err := bech32Decode("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4")
	if err != nil {
		t.Fatal(err)
	}
	if hrp != "bc" || len(data) == 0 || data[0] != 0 {
		t.Fatalf("hrp %s data %v", hrp, data)
	}
	program, err := convertBits(data[1:], 5, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	if !bytes.Equal(program, want) {
		t.Fatalf("program %x", program)
	}
}

func TestDecodeLnurlSpec(t *testing.T) {
	const encoded = "LNURL1DP68GURN8GHJ7UM9WFMXJCM99E3K7MF0V9CXJ0M385EKVCENXC6R2C35XVUKXEFCV5MKVV34X5EKZD3EV56NYD3HXQURZEPEXEJXXEPNXSCRVWFNV9NXZCN9XQ6XYEFHVGCXXCMYXYMNSERXFQ5FNS"
	const want = "https://service.com/api?q=3fc3645b439ce8e7f2553a69e5267081d96dcd340693afabe04be7b0ccd178df"
	got, err := decodeLnurl(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal(got)
	}
}

func TestDecodeLnurl(t *testing.T) {
	encoded := bech32Encode("lnurl", []byte("https://example.com/pay"))
	got, err := decodeLnurl("lightning:" + strings.ToUpper(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/pay" {
		t.Fatal(got)
	}
	if _, err := decodeLnurl("lnbc10n1example"); err == nil {
		t.Fatal("expected an invoice to be refused")
	}
}

func TestParseLnurlURI(t *testing.T) {
	encoded := bech32Encode("lnurl", []byte("https://example.com/pay"))
	got, err := parseBIP321("bitcoin:?lightning=" + encoded + "&amount=0.00001500")
	if err != nil {
		t.Fatal(err)
	}
	if got.lnurl != encoded || got.lightning != "" || !got.hasAmount || got.amountSat != 1500 {
		t.Fatalf("%+v", got)
	}
}

func bech32Encode(hrp string, payload []byte) string {
	data, err := convertBits(payload, 8, 5, true)
	if err != nil {
		panic(err)
	}
	values := append(bech32HrpExpand(hrp), data...)
	values = append(values, 0, 0, 0, 0, 0, 0)
	mod := bech32Polymod(values) ^ 1
	checksum := make([]byte, 6)
	for i := 0; i < 6; i++ {
		checksum[i] = byte((mod >> uint(5*(5-i))) & 31)
	}
	combined := append(data, checksum...)
	var b strings.Builder
	b.WriteString(hrp)
	b.WriteByte('1')
	for _, v := range combined {
		b.WriteByte(bech32Charset[v])
	}
	return b.String()
}
