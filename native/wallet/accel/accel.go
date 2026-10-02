package accel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Public mempool.space accelerator. No X-Mempool-Auth header: estimates and
// invoices are the anonymous public tier.
const mempoolAPI = "https://mempool.space/api"

var txidPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Default is the public mempool.space accelerator.
var Default = New(mempoolAPI)

type View struct {
	Txid          string `json:"txid"`
	Error         string `json:"error,omitempty"`
	EstimateError string `json:"estimate_error,omitempty"`

	HasEstimate    bool     `json:"has_estimate"`
	Unavailable    bool     `json:"unavailable"`
	EffectiveVsize uint64   `json:"effective_vsize"`
	EffectiveFee   uint64   `json:"effective_fee"`
	AncestorCount  uint64   `json:"ancestor_count"`
	Cost           uint64   `json:"cost"`
	MempoolBaseFee uint64   `json:"mempool_base_fee"`
	VsizeFee       uint64   `json:"vsize_fee"`
	TargetFeeRate  float64  `json:"target_fee_rate"`
	NextBlockFee   uint64   `json:"next_block_fee"`
	UserBalance    uint64   `json:"user_balance"`
	Pools          []int    `json:"pools"`
	PoolNames      []string `json:"pool_names"`
	Options        []uint64 `json:"options"`
	BitcoinEnabled bool     `json:"bitcoin_enabled"`
	BitcoinMin     uint64   `json:"bitcoin_min"`
	BitcoinMax     uint64   `json:"bitcoin_max"`

	HasBoost            bool     `json:"has_boost"`
	BoostStatus         string   `json:"boost_status,omitempty"`
	BoostFeeDelta       uint64   `json:"boost_fee_delta"`
	BoostAdded          int64    `json:"boost_added"`
	BoostEffectiveFee   uint64   `json:"boost_effective_fee"`
	BoostEffectiveVsize uint64   `json:"boost_effective_vsize"`
	BoostPools          []int    `json:"boost_pools"`
	BoostPoolNames      []string `json:"boost_pool_names"`

	HasInvoice bool   `json:"has_invoice"`
	Invoice    string `json:"invoice,omitempty"`
	BtcDue     string `json:"btc_due,omitempty"`
	DueSats    uint64 `json:"due_sats"`
	Expires    int64  `json:"expires"`
	InvoiceID  string `json:"invoice_id,omitempty"`
	QRPNG      string `json:"qr_png,omitempty"`
}

type Client struct {
	base    string
	http    *http.Client
	mu      sync.Mutex
	pools   map[int]string
	poolsAt time.Time
}

func New(base string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		http:  &http.Client{Timeout: 20 * time.Second},
		pools: map[int]string{},
	}
}

func Normalize(txid string) (string, error) {
	txid = strings.ToLower(strings.TrimSpace(txid))
	if !txidPattern.MatchString(txid) {
		return "", fmt.Errorf("This transaction id is not valid")
	}
	return txid, nil
}

func Empty(txid string) View {
	return View{
		Txid:           txid,
		Pools:          []int{},
		PoolNames:      []string{},
		Options:        []uint64{},
		BoostPools:     []int{},
		BoostPoolNames: []string{},
		BitcoinEnabled: true,
	}
}

func (c *Client) Preview(txid string) View {
	id, err := Normalize(txid)
	if err != nil {
		view := Empty(strings.TrimSpace(txid))
		view.Error = err.Error()
		return view
	}
	txid = id
	view := Empty(txid)
	est, err := c.estimate(txid)
	if err != nil {
		view.EstimateError = err.Error()
	} else {
		view = est
		view.Txid = txid
	}
	c.ensurePools()
	if len(view.Pools) > 0 {
		view.PoolNames = c.names(view.Pools)
	}
	boost, found, err := c.findPending(txid)
	if err != nil {
		view.Error = err.Error()
	} else if found {
		view.HasBoost = true
		view.BoostStatus = "accelerating"
		view.BoostFeeDelta = boost.FeeDelta
		view.BoostAdded = boost.Added
		view.BoostEffectiveFee = boost.EffectiveFee
		view.BoostEffectiveVsize = boost.EffectiveVsize
		view.BoostPools = boost.Pools
		view.BoostPoolNames = c.names(boost.Pools)
	}
	return view
}

type Invoice struct {
	Invoice   string
	BtcDue    string
	DueSats   uint64
	Expires   int64
	InvoiceID string
}

func (c *Client) CreateInvoice(txid string, maxBid uint64) (Invoice, error) {
	id, err := Normalize(txid)
	if err != nil {
		return Invoice{}, err
	}
	txid = id
	if maxBid == 0 {
		return Invoice{}, fmt.Errorf("Choose a boost price first")
	}
	form := url.Values{}
	form.Set("txid", txid)
	form.Set("maxBidBoost", strconv.FormatUint(maxBid, 10))
	body, status, err := c.call(http.MethodPost, "/v1/services/accelerator/invoice", form)
	if err != nil {
		return Invoice{}, err
	}
	if status != http.StatusOK {
		return Invoice{}, friendlyAccelError(status, body)
	}
	return parseInvoice(body)
}

// WithInvoice records a CreateInvoice result on a preview without dropping the quote.
func (v View) WithInvoice(inv Invoice, callErr error) View {
	if callErr != nil {
		v.Error = callErr.Error()
		v.HasInvoice = false
		v.Invoice = ""
		v.QRPNG = ""
		v.DueSats = 0
		v.Expires = 0
		v.InvoiceID = ""
		v.BtcDue = ""
		return v
	}
	v.Error = ""
	v.HasInvoice = true
	v.Invoice = inv.Invoice
	v.BtcDue = inv.BtcDue
	v.DueSats = inv.DueSats
	v.Expires = inv.Expires
	v.InvoiceID = inv.InvoiceID
	return v
}

func (c *Client) estimate(txid string) (View, error) {
	form := url.Values{}
	form.Set("txid", txid)
	body, status, err := c.call(http.MethodPost, "/v1/services/accelerator/estimate", form)
	if err != nil {
		return View{}, err
	}
	if status != http.StatusOK {
		return View{}, friendlyAccelError(status, body)
	}
	return parseEstimate(body)
}

type pendingBoost struct {
	FeeDelta       uint64
	Added          int64
	EffectiveFee   uint64
	EffectiveVsize uint64
	Pools          []int
}

func (c *Client) findPending(txid string) (pendingBoost, bool, error) {
	body, status, err := c.call(http.MethodGet, "/v1/services/accelerator/accelerations", nil)
	if err != nil {
		return pendingBoost{}, false, err
	}
	if status != http.StatusOK {
		return pendingBoost{}, false, friendlyAccelError(status, body)
	}
	var list []struct {
		Txid           string    `json:"txid"`
		Added          float64   `json:"added"`
		FeeDelta       float64   `json:"feeDelta"`
		EffectiveVsize float64   `json:"effectiveVsize"`
		EffectiveFee   float64   `json:"effectiveFee"`
		Pools          []float64 `json:"pools"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return pendingBoost{}, false, fmt.Errorf("mempool accelerator returned an unreadable boost list")
	}
	for _, item := range list {
		if !strings.EqualFold(item.Txid, txid) {
			continue
		}
		return pendingBoost{
			FeeDelta:       u64(item.FeeDelta),
			Added:          int64(item.Added),
			EffectiveFee:   u64(item.EffectiveFee),
			EffectiveVsize: u64(item.EffectiveVsize),
			Pools:          ints(item.Pools),
		}, true, nil
	}
	return pendingBoost{}, false, nil
}

func (c *Client) call(method, path string, form url.Values) ([]byte, int, error) {
	var reader io.Reader
	if form != nil {
		reader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Barkauf")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("mempool accelerator is unreachable")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("mempool accelerator response was cut off")
	}
	return body, resp.StatusCode, nil
}

func (c *Client) ensurePools() {
	c.mu.Lock()
	fresh := len(c.pools) > 0 && time.Since(c.poolsAt) < 6*time.Hour
	c.mu.Unlock()
	if fresh {
		return
	}
	body, status, err := c.call(http.MethodGet, "/v1/mining/pools", nil)
	if err != nil || status != http.StatusOK {
		return
	}
	var list []struct {
		Name string `json:"name"`
		ID   int    `json:"unique_id"`
	}
	if json.Unmarshal(body, &list) != nil || len(list) == 0 {
		return
	}
	next := make(map[int]string, len(list))
	for _, pool := range list {
		if pool.Name != "" {
			next[pool.ID] = pool.Name
		}
	}
	if len(next) == 0 {
		return
	}
	c.mu.Lock()
	c.pools = next
	c.poolsAt = time.Now()
	c.mu.Unlock()
}

func (c *Client) names(ids []int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := c.pools[id]; name != "" {
			out = append(out, name)
		} else if id != 0 {
			out = append(out, fmt.Sprintf("Pool %d", id))
		}
	}
	return out
}

type estimatePayload struct {
	TxSummary struct {
		Txid           string  `json:"txid"`
		EffectiveVsize float64 `json:"effectiveVsize"`
		EffectiveFee   float64 `json:"effectiveFee"`
		AncestorCount  float64 `json:"ancestorCount"`
	} `json:"txSummary"`
	Cost           float64   `json:"cost"`
	MempoolBaseFee float64   `json:"mempoolBaseFee"`
	VsizeFee       float64   `json:"vsizeFee"`
	TargetFeeRate  float64   `json:"targetFeeRate"`
	NextBlockFee   float64   `json:"nextBlockFee"`
	UserBalance    float64   `json:"userBalance"`
	Pools          []float64 `json:"pools"`
	Options        []struct {
		Fee float64 `json:"fee"`
	} `json:"options"`
	Available *struct {
		Bitcoin struct {
			Enabled bool    `json:"enabled"`
			Min     float64 `json:"min"`
			Max     float64 `json:"max"`
		} `json:"bitcoin"`
	} `json:"availablePaymentMethods"`
	Unavailable bool `json:"unavailable"`
}

func parseEstimate(body []byte) (View, error) {
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 || trim[0] != '{' {
		return View{}, friendlyAccelError(http.StatusOK, trim)
	}
	var payload estimatePayload
	if err := json.Unmarshal(trim, &payload); err != nil {
		return View{}, fmt.Errorf("mempool accelerator returned an unreadable estimate")
	}
	view := Empty(strings.ToLower(payload.TxSummary.Txid))
	view.HasEstimate = true
	view.Unavailable = payload.Unavailable
	view.EffectiveVsize = u64(payload.TxSummary.EffectiveVsize)
	view.EffectiveFee = u64(payload.TxSummary.EffectiveFee)
	view.AncestorCount = u64(payload.TxSummary.AncestorCount)
	view.Cost = u64(payload.Cost)
	view.MempoolBaseFee = u64(payload.MempoolBaseFee)
	view.VsizeFee = u64(payload.VsizeFee)
	view.TargetFeeRate = payload.TargetFeeRate
	view.NextBlockFee = u64(payload.NextBlockFee)
	view.UserBalance = u64(payload.UserBalance)
	view.Pools = ints(payload.Pools)
	view.BitcoinEnabled = true
	if payload.Available != nil {
		view.BitcoinEnabled = payload.Available.Bitcoin.Enabled
		view.BitcoinMin = u64(payload.Available.Bitcoin.Min)
		view.BitcoinMax = u64(payload.Available.Bitcoin.Max)
	}
	seen := map[uint64]bool{}
	for _, option := range payload.Options {
		fee := u64(option.Fee)
		if fee == 0 || seen[fee] {
			continue
		}
		seen[fee] = true
		view.Options = append(view.Options, fee)
	}
	if len(view.Options) == 0 && view.Cost > 0 && !view.Unavailable {
		view.Options = []uint64{view.Cost}
	}
	return view, nil
}

func parseInvoice(body []byte) (Invoice, error) {
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 || trim[0] != '{' {
		return Invoice{}, friendlyAccelError(http.StatusOK, trim)
	}
	var payload struct {
		ID        string          `json:"btcpayInvoiceId"`
		BtcDue    json.RawMessage `json:"btcDue"`
		Expires   float64         `json:"expirationTime"`
		Bolt11    string          `json:"bolt11"`
		Addresses struct {
			Lightning string `json:"BTC_LightningLike"`
		} `json:"addresses"`
	}
	if err := json.Unmarshal(trim, &payload); err != nil {
		return Invoice{}, fmt.Errorf("mempool accelerator returned an unreadable invoice")
	}
	invoice := strings.TrimSpace(payload.Addresses.Lightning)
	if invoice == "" {
		invoice = strings.TrimSpace(payload.Bolt11)
	}
	if invoice == "" {
		return Invoice{}, fmt.Errorf("mempool accelerator did not return a Lightning invoice")
	}
	dueText := strings.Trim(strings.TrimSpace(string(payload.BtcDue)), `"`)
	sats := uint64(0)
	if n, ok := bolt11Sats(invoice); ok && n > 0 {
		sats = n
	} else if dueText != "" {
		if n, err := btcToSats(dueText); err == nil {
			sats = n
		}
	}
	if sats == 0 {
		return Invoice{}, fmt.Errorf("mempool accelerator invoice has no amount")
	}
	return Invoice{
		Invoice:   invoice,
		BtcDue:    dueText,
		DueSats:   sats,
		Expires:   int64(payload.Expires),
		InvoiceID: payload.ID,
	}, nil
}

func friendlyAccelError(status int, body []byte) error {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("mempool accelerator returned HTTP %d", status)
	}
	if strings.HasPrefix(text, "{") {
		var wrap struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &wrap) == nil {
			if wrap.Error != "" {
				text = wrap.Error
			} else if wrap.Message != "" {
				text = wrap.Message
			}
		}
	}
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(text)
	switch text {
	case "cannot_accelerate_tx":
		return fmt.Errorf("This transaction cannot be accelerated. It may already be confirmed, already boosted, or not in the mempool")
	case "invalid_txid":
		return fmt.Errorf("This transaction id is not valid")
	case "no_tx_found", "txid_not_found", "txid_not_in_mempool":
		return fmt.Errorf("This transaction is not in the mempool")
	}
	if len(text) > 180 {
		text = text[:180]
	}
	text = strings.ReplaceAll(text, "_", " ")
	return fmt.Errorf("%s", text)
}

func u64(n float64) uint64 {
	if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return uint64(math.Round(n))
}

func ints(list []float64) []int {
	out := make([]int, 0, len(list))
	for _, n := range list {
		if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			continue
		}
		out = append(out, int(math.Round(n)))
	}
	return out
}

func btcToSats(s string) (uint64, error) {
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

// InvoiceSats reads the amount encoded in a BOLT11 invoice.
// The boolean is false when the invoice is amountless or not a bolt11 string.
func InvoiceSats(invoice string) (uint64, bool) {
	return bolt11Sats(invoice)
}

func bolt11Sats(invoice string) (uint64, bool) {
	s := strings.ToLower(strings.TrimSpace(invoice))
	rest := ""
	for _, prefix := range []string{"lnbcrt", "lntbs", "lnbc", "lntb"} {
		if strings.HasPrefix(s, prefix) {
			rest = strings.TrimPrefix(s, prefix)
			break
		}
	}
	if rest == "" {
		return 0, false
	}
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	digits := rest[:i]
	mult := byte(0)
	if i < len(rest) && strings.ContainsRune("munp", rune(rest[i])) {
		mult = rest[i]
		i++
	}
	if i >= len(rest) || rest[i] != '1' {
		return 0, false
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	var scale uint64
	switch mult {
	case 'm':
		scale = 100000
	case 'u':
		scale = 100
	case 'n':
		return (n + 5) / 10, true
	case 'p':
		return (n + 5000) / 10000, true
	default:
		scale = 100000000
	}
	if n > ^uint64(0)/scale {
		return 0, false
	}
	return n * scale, true
}
