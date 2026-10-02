package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gitlab.com/ark-bitcoin/bark-ffi-bindings/golang/bark"
)

// Noah accepts an email-shaped Lightning address, lowercased, and refuses
// onion domains. The user part is checked again after the address matches.
var (
	lnEmail = regexp.MustCompile(`^[a-z0-9._-]+@[a-z0-9.-]+\.[a-z]{2,4}$`)
	lnUser  = regexp.MustCompile(`^[a-z0-9_.-]+$`)
)

type lnurlPay struct {
	minMsat uint64
	maxMsat uint64
	ark     string
}

func splitLightningAddress(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), "lightning:") {
		s = strings.TrimSpace(s[len("lightning:"):])
	}
	s = strings.ToLower(s)
	if !lnEmail.MatchString(s) {
		return "", "", fmt.Errorf("not a Lightning address")
	}
	user, domain, _ := strings.Cut(s, "@")
	if !lnUser.MatchString(user) || strings.HasSuffix(domain, ".onion") {
		return "", "", fmt.Errorf("not a Lightning address")
	}
	return user, domain, nil
}

func (p lnurlPay) allows(sats uint64) error {
	if sats == 0 || sats > math.MaxUint64/1000 {
		return fmt.Errorf("amount is outside the range for this Lightning address")
	}
	msat := sats * 1000
	if msat < p.minMsat || msat > p.maxMsat {
		return fmt.Errorf("amount is outside the range for this Lightning address")
	}
	return nil
}

func lnurlEndpoint(user, domain, arkPubkey string) string {
	endpoint := "https://" + domain + "/.well-known/lnurlp/" + url.PathEscape(user)
	if arkPubkey != "" {
		endpoint += "?ark=" + url.QueryEscape(arkPubkey)
	}
	return endpoint
}

func fetchLnurlPay(user, domain, arkPubkey string) (lnurlPay, error) {
	body, err := getHTTPS(lnurlEndpoint(user, domain, arkPubkey))
	if err != nil {
		return lnurlPay{}, err
	}
	return parseLnurlPay(body)
}

func parseLnurlPay(body []byte) (lnurlPay, error) {
	var raw struct {
		Callback    string  `json:"callback"`
		MaxSendable float64 `json:"maxSendable"`
		MinSendable float64 `json:"minSendable"`
		Tag         string  `json:"tag"`
		Ark         string  `json:"ark"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return lnurlPay{}, err
	}
	if raw.Tag != "payRequest" || strings.TrimSpace(raw.Callback) == "" {
		return lnurlPay{}, fmt.Errorf("Lightning address did not return a pay request")
	}
	min, err := jsonMsat(raw.MinSendable)
	if err != nil {
		return lnurlPay{}, err
	}
	max, err := jsonMsat(raw.MaxSendable)
	if err != nil {
		return lnurlPay{}, err
	}
	if max == 0 || min > max {
		return lnurlPay{}, fmt.Errorf("Lightning address returned an invalid amount range")
	}
	return lnurlPay{minMsat: min, maxMsat: max, ark: strings.TrimSpace(raw.Ark)}, nil
}

func jsonMsat(n float64) (uint64, error) {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n != math.Trunc(n) {
		return 0, fmt.Errorf("Lightning address returned an invalid amount range")
	}
	return uint64(n), nil
}

func lightningURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("Lightning lookup returned an invalid address")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".onion") {
		return nil
	}
	return fmt.Errorf("Lightning lookup left HTTPS")
}

func getHTTPS(raw string) ([]byte, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := lightningURL(req.URL.String()); err != nil {
				return err
			}
			if len(via) >= 5 {
				return fmt.Errorf("Lightning lookup redirected too many times")
			}
			return nil
		},
	}
	resp, err := client.Get(raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("Lightning lookup returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (st *walletState) lookupLightningAddress(addr string) (lnurlPay, error) {
	user, domain, err := splitLightningAddress(addr)
	if err != nil {
		return lnurlPay{}, err
	}
	progress("Resolving Lightning address")
	pubkey := ""
	if info := st.arkInfo(); info != nil {
		pubkey = strings.Clone(info.ServerPubkey)
		info.Destroy()
	}
	pay, err := fetchLnurlPay(user, domain, pubkey)
	if err != nil {
		return lnurlPay{}, err
	}
	if pay.ark != "" {
		ok, verr := st.wallet.ValidateArkoorAddress(pay.ark)
		if verr != nil || !ok {
			if verr != nil {
				fmt.Fprintln(os.Stderr, "lightning address ark:", verr)
			}
			pay.ark = ""
		}
	}
	return pay, nil
}

func (st *walletState) estimateLightningFee(dest string, amount uint64) (bark.FeeEstimate, error) {
	if _, _, err := splitLightningAddress(dest); err == nil {
		pay, err := st.lookupLightningAddress(dest)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lightning address:", err)
			return st.wallet.EstimateLightningSendFee(amount)
		}
		if err := pay.allows(amount); err != nil {
			return bark.FeeEstimate{}, err
		}
		if pay.ark != "" {
			return st.wallet.EstimateArkoorPaymentFee(amount)
		}
	}
	return st.wallet.EstimateLightningSendFee(amount)
}

func (st *walletState) payLightningAddress(addr string, amount uint64) (string, error) {
	pay, err := st.lookupLightningAddress(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lightning address:", err)
		return st.finishLightningAddress(addr, amount)
	}
	if err := pay.allows(amount); err != nil {
		return "", err
	}
	if pay.ark != "" {
		progress("Paying on Ark")
		if err := st.wallet.SendArkoorPayment(pay.ark, amount); err != nil {
			return "", err
		}
		return "sent", nil
	}
	return st.finishLightningAddress(addr, amount)
}

func (st *walletState) finishLightningAddress(addr string, amount uint64) (string, error) {
	progress("Paying Lightning address")
	status, err := st.wallet.PayLightningAddress(addr, amount, nil, true)
	if err != nil {
		return "", err
	}
	if err := settledLightning(status); err != nil {
		return "", err
	}
	return "paid", nil
}
