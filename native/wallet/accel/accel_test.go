package accel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEstimatePublicSample(t *testing.T) {
	body := []byte(`{
		"txSummary": {"txid": "EE13EBB99632377C15C94980357F674D285AC413452050031EA6DCD3E9B2DC29", "effectiveVsize": 154, "effectiveFee": 154, "ancestorCount": 1},
		"cost": 1386,
		"targetFeeRate": 10,
		"nextBlockFee": 1540,
		"userBalance": 0,
		"mempoolBaseFee": 50000,
		"vsizeFee": 0,
		"pools": [111, 102, 112],
		"options": [{"fee": 1500}, {"fee": 3000}, {"fee": 12500}],
		"availablePaymentMethods": {"bitcoin": {"enabled": true, "min": 1000, "max": 10000000}},
		"unavailable": false
	}`)
	view, err := parseEstimate(body)
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasEstimate || view.Unavailable || !view.BitcoinEnabled {
		t.Fatalf("flags %+v", view)
	}
	if view.EffectiveVsize != 154 || view.EffectiveFee != 154 || view.AncestorCount != 1 {
		t.Fatalf("summary %+v", view)
	}
	if view.Cost != 1386 || view.MempoolBaseFee != 50000 || view.TargetFeeRate != 10 || view.NextBlockFee != 1540 {
		t.Fatalf("fees %+v", view)
	}
	if view.BitcoinMin != 1000 || view.BitcoinMax != 10000000 {
		t.Fatalf("limits %+v", view)
	}
	if len(view.Options) != 3 || view.Options[0] != 1500 || view.Options[2] != 12500 {
		t.Fatalf("options %v", view.Options)
	}
	if len(view.Pools) != 3 || view.Pools[0] != 111 {
		t.Fatalf("pools %v", view.Pools)
	}
	if view.Txid != "ee13ebb99632377c15c94980357f674d285ac413452050031ea6dcd3e9b2dc29" {
		t.Fatalf("txid %s", view.Txid)
	}
}

func TestParseInvoiceNumericDue(t *testing.T) {
	inv, err := parseInvoice([]byte(`{
		"btcpayInvoiceId": "9hxWf6t67mi6W73hHj36Ps",
		"btcDue": 0.00076,
		"addresses": {"BTC_LightningLike": "lnbc760u1p4ta37qpp5example"},
		"expirationTime": 1790888770
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if inv.DueSats != 76000 {
		t.Fatalf("due %d", inv.DueSats)
	}
	if inv.BtcDue != "0.00076" || inv.Expires != 1790888770 || inv.InvoiceID == "" {
		t.Fatalf("%+v", inv)
	}
}

func TestParseInvoiceStringDue(t *testing.T) {
	inv, err := parseInvoice([]byte(`{
		"btcpayInvoiceId": "abc",
		"btcDue": "0.002875",
		"addresses": {"BTC_LightningLike": "lnbc2875u1p5ax5v4example"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if inv.DueSats != 287500 {
		t.Fatalf("due %d", inv.DueSats)
	}
}

func TestFriendlyAccelError(t *testing.T) {
	err := friendlyAccelError(400, []byte("cannot_accelerate_tx"))
	if err == nil || !strings.Contains(err.Error(), "cannot be accelerated") {
		t.Fatal(err)
	}
}

func TestAccelPreviewAndInvoice(t *testing.T) {
	const txid = "38bdd9437578c001055d3a6984ff42f470ca09f6cd0f49b42fdcafc416e1348e"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/services/accelerator/estimate":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("txid") != txid {
				t.Errorf("estimate txid %q", r.Form.Get("txid"))
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{
				"txSummary": {"txid": "`+txid+`", "effectiveVsize": 141, "effectiveFee": 564, "ancestorCount": 1},
				"cost": 1000, "targetFeeRate": 4, "nextBlockFee": 564, "userBalance": 0,
				"pools": [111, 4],
				"options": [{"fee": 1000}, {"fee": 2000}],
				"availablePaymentMethods": {"bitcoin": {"enabled": true, "min": 1000, "max": 10000000}},
				"unavailable": false
			}`)
		case "/v1/services/accelerator/accelerations":
			io.WriteString(w, `[{
				"txid": "`+txid+`",
				"added": 1707558316,
				"feeDelta": 3500,
				"effectiveVsize": 141,
				"effectiveFee": 1671,
				"pools": [111]
			}]`)
		case "/v1/mining/pools":
			io.WriteString(w, `[{"name":"Foundry USA","slug":"foundryusa","unique_id":111},{"name":"Luxor","slug":"luxor","unique_id":4}]`)
		case "/v1/services/accelerator/invoice":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("maxBidBoost") != "1000" {
				t.Errorf("bid %q", r.Form.Get("maxBidBoost"))
			}
			io.WriteString(w, `{
				"btcpayInvoiceId": "inv1",
				"btcDue": 0.00076,
				"addresses": {"BTC_LightningLike": "lnbc760u1p4ta37qpp5example"},
				"expirationTime": 1790888770
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL)
	view := c.Preview(txid)
	if !view.HasEstimate || !view.HasBoost {
		t.Fatalf("preview %+v", view)
	}
	if view.BoostFeeDelta != 3500 || view.BoostAdded != 1707558316 || view.BoostEffectiveFee != 1671 {
		t.Fatalf("boost %+v", view)
	}
	if len(view.PoolNames) != 2 || view.PoolNames[0] != "Foundry USA" || view.PoolNames[1] != "Luxor" {
		t.Fatalf("pools %v", view.PoolNames)
	}
	if len(view.BoostPoolNames) != 1 || view.BoostPoolNames[0] != "Foundry USA" {
		t.Fatalf("boost pools %v", view.BoostPoolNames)
	}
	if view.EffectiveFee != 564 || view.Options[1] != 2000 {
		t.Fatalf("estimate kept %+v", view)
	}

	inv, err := c.CreateInvoice(txid, 1000)
	if err != nil {
		t.Fatal(err)
	}
	got := view.WithInvoice(inv, nil)
	if !got.HasInvoice || got.DueSats != 76000 {
		t.Fatalf("invoice %+v", got)
	}
	if got.EffectiveVsize != 141 || !got.HasBoost || got.BoostFeeDelta != 3500 {
		t.Fatalf("invoice dropped estimate %+v", got)
	}
	if !strings.HasPrefix(got.Invoice, "lnbc760u") {
		t.Fatalf("invoice %s", got.Invoice)
	}
}

func TestAccelEstimateErrorKeepsBoost(t *testing.T) {
	const txid = "ee13ebb99632377c15c94980357f674d285ac413452050031ea6dcd3e9b2dc29"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/services/accelerator/estimate":
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, "cannot_accelerate_tx")
		case "/v1/services/accelerator/accelerations":
			io.WriteString(w, `[{"txid":"`+txid+`","added":10,"feeDelta":500,"effectiveVsize":100,"effectiveFee":200,"pools":[4]}]`)
		case "/v1/mining/pools":
			io.WriteString(w, `[{"name":"Luxor","unique_id":4}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	view := New(srv.URL).Preview(txid)
	if view.HasEstimate || !view.HasBoost {
		t.Fatalf("%+v", view)
	}
	if !strings.Contains(view.EstimateError, "cannot be accelerated") {
		t.Fatalf("error %q", view.EstimateError)
	}
	if view.BoostPoolNames[0] != "Luxor" || view.BoostFeeDelta != 500 {
		t.Fatalf("boost %+v", view)
	}
}
