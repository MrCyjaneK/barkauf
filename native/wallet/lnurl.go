package main

import (
	"fmt"
	"os"
	"strings"
)

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// A fixed LNURL-pay amount is not written on the code. The pay request
// states it by making the minimum and maximum the same millisatoshi value.
func fixedLnurlSats(pay lnurlPay) (uint64, bool) {
	if pay.minMsat == 0 || pay.minMsat != pay.maxMsat || pay.minMsat%1000 != 0 {
		return 0, false
	}
	return pay.minMsat / 1000, true
}

func looksLikeLnurl(raw string) bool {
	_, err := normalizeLnurl(raw)
	return err == nil
}

func normalizeLnurl(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), "lightning:") {
		s = strings.TrimSpace(s[len("lightning:"):])
		s = strings.TrimLeft(s, "/")
	}
	if s != strings.ToLower(s) && s != strings.ToUpper(s) {
		return "", fmt.Errorf("not an LNURL")
	}
	if !strings.HasPrefix(strings.ToLower(s), "lnurl1") {
		return "", fmt.Errorf("not an LNURL")
	}
	if len(s) < len("lnurl1")+6 || len(s) > 2048 {
		return "", fmt.Errorf("not an LNURL")
	}
	return s, nil
}

func fetchLnurlLink(link string) (lnurlPay, error) {
	endpoint, err := decodeLnurl(link)
	if err != nil {
		return lnurlPay{}, err
	}
	endpoint = strings.TrimSpace(endpoint)
	if err := lightningURL(endpoint); err != nil {
		return lnurlPay{}, err
	}
	body, err := getHTTPS(endpoint)
	if err != nil {
		return lnurlPay{}, err
	}
	return parseLnurlPay(body)
}

func (st *walletState) resolveLnurl(raw string) response {
	link, err := normalizeLnurl(raw)
	if err != nil {
		return response{OK: true, Lnurl: strings.TrimSpace(raw)}
	}
	progress("Resolving Lightning link")
	pay, err := fetchLnurlLink(link)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lnurl:", err)
		return response{OK: true, Lnurl: link}
	}
	sats, fixed := fixedLnurlSats(pay)
	return response{OK: true, Lnurl: link, LnurlFixed: fixed, LnurlAmount: sats}
}

func (st *walletState) payLnurl(link string, amount uint64) (string, error) {
	progress("Paying Lightning link")
	status, err := st.wallet.PayLnurl(link, amount, nil, true)
	if err != nil {
		return "", err
	}
	if err := settledLightning(status); err != nil {
		return "", err
	}
	return "paid", nil
}

func decodeLnurl(raw string) (string, error) {
	s, err := normalizeLnurl(raw)
	if err != nil {
		return "", err
	}
	hrp, data, err := bech32Decode(s)
	if err != nil {
		return "", err
	}
	if hrp != "lnurl" {
		return "", fmt.Errorf("not an LNURL")
	}
	decoded, err := convertBits(data, 5, 8, false)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func bech32Decode(s string) (string, []byte, error) {
	if s != strings.ToLower(s) && s != strings.ToUpper(s) {
		return "", nil, fmt.Errorf("not an LNURL")
	}
	s = strings.ToLower(s)
	pos := strings.LastIndexByte(s, '1')
	if pos < 1 || pos+7 > len(s) {
		return "", nil, fmt.Errorf("not an LNURL")
	}
	hrp := s[:pos]
	payload := s[pos+1:]
	data := make([]byte, len(payload))
	for i := 0; i < len(payload); i++ {
		n := strings.IndexByte(bech32Charset, payload[i])
		if n < 0 {
			return "", nil, fmt.Errorf("not an LNURL")
		}
		data[i] = byte(n)
	}
	if bech32Polymod(append(bech32HrpExpand(hrp), data...)) != 1 {
		return "", nil, fmt.Errorf("not an LNURL")
	}
	return hrp, data[:len(data)-6], nil
}

func bech32Polymod(values []byte) uint32 {
	gen := [...]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HrpExpand(hrp string) []byte {
	out := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

func convertBits(data []byte, fromBits, toBits uint, pad bool) ([]byte, error) {
	acc := uint32(0)
	bits := uint(0)
	maxv := uint32((1 << toBits) - 1)
	maxAcc := uint32((1 << (fromBits + toBits - 1)) - 1)
	out := make([]byte, 0, len(data)*int(fromBits)/int(toBits)+1)
	for _, value := range data {
		if uint32(value)>>fromBits != 0 {
			return nil, fmt.Errorf("not an LNURL")
		}
		acc = ((acc << fromBits) | uint32(value)) & maxAcc
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			out = append(out, byte((acc>>bits)&maxv))
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte((acc<<(toBits-bits))&maxv))
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, fmt.Errorf("not an LNURL")
	}
	return out, nil
}
