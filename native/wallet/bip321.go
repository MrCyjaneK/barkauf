package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// bip321URI builds a mainnet BIP 321 URI. The on-chain address is the URI
// body, matching bark's builder for Bitcoin mainnet.
func bip321URI(ark, onchain, bolt11 string, amountSat uint64) string {
	q := url.Values{}
	if ark != "" {
		q.Set("ark", ark)
	}
	if bolt11 != "" {
		q.Set("lightning", bolt11)
	}
	if amountSat > 0 {
		q.Set("amount", formatBTC(amountSat))
	}
	uri := "bitcoin:" + onchain
	if enc := q.Encode(); enc != "" {
		uri += "?" + enc
	}
	return uri
}

func formatBTC(sats uint64) string {
	return fmt.Sprintf("%d.%08d", sats/100000000, sats%100000000)
}

func parseBTC(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	parts := strings.SplitN(s, ".", 2)
	whole := uint64(0)
	var err error
	if parts[0] != "" {
		whole, err = strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
	}
	frac := uint64(0)
	if len(parts) == 2 {
		f := parts[1]
		if len(f) > 8 {
			return 0, fmt.Errorf("amount has more than 8 decimal places")
		}
		for len(f) < 8 {
			f += "0"
		}
		frac, err = strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	if whole > (^uint64(0)-frac)/100000000 {
		return 0, fmt.Errorf("amount overflows")
	}
	return whole*100000000 + frac, nil
}

type bip321 struct {
	lightning string
	lnaddr    string
	lnurl     string
	ark       string
	onchain   string
	amountSat uint64
	hasAmount bool
}

func parseBIP321(raw string) (bip321, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return bip321{}, err
	}
	if !strings.EqualFold(u.Scheme, "bitcoin") {
		return bip321{}, fmt.Errorf("not a bitcoin URI")
	}
	q := u.Query()
	lightning := q.Get("lightning")
	lnurl := q.Get("lnurl")
	if lnurl == "" && looksLikeLnurl(lightning) {
		lnurl = lightning
		lightning = ""
	}
	out := bip321{
		lightning: lightning,
		lnaddr:    q.Get("lnaddr"),
		lnurl:     lnurl,
		ark:       q.Get("ark"),
		onchain:   firstNonEmpty(q.Get("bc"), q.Get("tb")),
	}
	if out.onchain == "" {
		body := u.Opaque
		if body == "" {
			body = u.Host
		}
		body = strings.TrimPrefix(body, "?")
		if i := strings.IndexByte(body, '?'); i >= 0 {
			body = body[:i]
		}
		out.onchain = body
	}
	if a := q.Get("amount"); a != "" {
		sats, err := parseBTC(a)
		if err != nil {
			return bip321{}, err
		}
		out.amountSat = sats
		out.hasAmount = true
	}
	if out.lightning == "" && out.lnaddr == "" && out.lnurl == "" && out.ark == "" && out.onchain == "" {
		return bip321{}, fmt.Errorf("URI has no payment destination")
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
