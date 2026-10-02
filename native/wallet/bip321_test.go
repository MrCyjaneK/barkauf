package main

import "testing"

func TestBIP321RoundTrip(t *testing.T) {
	raw := bip321URI("ark1example", "bc1qexample", "lnbc10n1example", 1500)
	got, err := parseBIP321(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ark != "ark1example" || got.onchain != "bc1qexample" || got.lightning != "lnbc10n1example" {
		t.Fatalf("parsed %+v", got)
	}
	if !got.hasAmount || got.amountSat != 1500 {
		t.Fatalf("amount %+v", got)
	}
}

func TestParseBodyAddress(t *testing.T) {
	got, err := parseBIP321("bitcoin:bc1qbody?amount=0.00000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.onchain != "bc1qbody" || got.amountSat != 1 {
		t.Fatalf("parsed %+v", got)
	}
}

func TestParseBTCOverflow(t *testing.T) {
	if _, err := parseBTC("184467440738"); err == nil {
		t.Fatal("expected overflow")
	}
	got, err := parseBTC("0.00000001")
	if err != nil || got != 1 {
		t.Fatalf("got %d %v", got, err)
	}
}

func TestLightningPayAmount(t *testing.T) {
	amount, err := lightningPayAmount("lnbc10n1example", 1, true)
	if err != nil || amount != nil {
		t.Fatalf("matching invoice amount should be left on the invoice: %v %v", amount, err)
	}
	if _, err := lightningPayAmount("lnbc10n1example", 2, true); err == nil {
		t.Fatal("expected mismatch")
	}
	amount, err = lightningPayAmount("lnbc1example", 1500, true)
	if err != nil || amount == nil || *amount != 1500 {
		t.Fatalf("amountless invoice: %v %v", amount, err)
	}
	if _, err := lightningPayAmount("lnbc1example", 0, false); err == nil {
		t.Fatal("expected missing amount")
	}
}

func TestSatVbFromKwu(t *testing.T) {
	if satVbFromKwu(0) != 1 || satVbFromKwu(250) != 1 || satVbFromKwu(251) != 2 || satVbFromKwu(2500) != 10 {
		t.Fatalf("rates %d %d %d %d", satVbFromKwu(0), satVbFromKwu(250), satVbFromKwu(251), satVbFromKwu(2500))
	}
}

func TestParseLightningAddressURI(t *testing.T) {
	got, err := parseBIP321("bitcoin:?lnaddr=uwu%40cake.cash&amount=0.00000010")
	if err != nil {
		t.Fatal(err)
	}
	if got.lnaddr != "uwu@cake.cash" || got.lightning != "" || got.onchain != "" {
		t.Fatalf("parsed %+v", got)
	}
	if !got.hasAmount || got.amountSat != 10 {
		t.Fatalf("amount %+v", got)
	}
}

func TestParseLnurlParam(t *testing.T) {
	got, err := parseBIP321("bitcoin:?lnurl=lnurl1qqqqqqqq&amount=0.00000010")
	if err != nil {
		t.Fatal(err)
	}
	if got.lnurl != "lnurl1qqqqqqqq" || got.lightning != "" || got.amountSat != 10 {
		t.Fatalf("%+v", got)
	}
}

func TestArkWithoutAmount(t *testing.T) {
	got, err := parseBIP321("bitcoin:?ark=ark1only")
	if err != nil {
		t.Fatal(err)
	}
	if got.hasAmount {
		t.Fatal("expected no amount")
	}
}
