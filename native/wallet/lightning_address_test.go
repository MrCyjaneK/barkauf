package main

import "testing"

func TestSplitLightningAddress(t *testing.T) {
	user, domain, err := splitLightningAddress("  Uwu@Cake.cash ")
	if err != nil || user != "uwu" || domain != "cake.cash" {
		t.Fatalf("got %q %q %v", user, domain, err)
	}
	user, domain, err = splitLightningAddress("lightning:uwu@cake.cash")
	if err != nil || user != "uwu" || domain != "cake.cash" {
		t.Fatalf("prefixed got %q %q %v", user, domain, err)
	}
	for _, bad := range []string{"", "uwu", "uwu@cake", "not an address", "uwu@cake.onion", "lnbc1example"} {
		if _, _, err := splitLightningAddress(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestLnurlPayAmount(t *testing.T) {
	pay := lnurlPay{minMsat: 1000, maxMsat: 100000000000}
	if err := pay.allows(1); err != nil {
		t.Fatal(err)
	}
	if err := pay.allows(0); err == nil {
		t.Fatal("expected zero to be refused")
	}
	if err := pay.allows(100000000001); err == nil {
		t.Fatal("expected amount above the maximum to be refused")
	}
}

func TestLnurlEndpoint(t *testing.T) {
	got := lnurlEndpoint("uwu", "cake.cash", "serverpubkey")
	if got != "https://cake.cash/.well-known/lnurlp/uwu?ark=serverpubkey" {
		t.Fatal(got)
	}
}

func TestParseLnurlPayArk(t *testing.T) {
	pay, err := parseLnurlPay([]byte(`{"callback":"https://cake.cash/lnurlp/uwu/invoice","maxSendable":100000000000,"minSendable":1000,"tag":"payRequest","ark":"ark1example"}`))
	if err != nil {
		t.Fatal(err)
	}
	if pay.ark != "ark1example" || pay.minMsat != 1000 || pay.maxMsat != 100000000000 {
		t.Fatalf("%+v", pay)
	}
}

func TestParseLnurlPayRejectsNonPay(t *testing.T) {
	if _, err := parseLnurlPay([]byte(`{"tag":"withdrawRequest","callback":"https://example.com"}`)); err == nil {
		t.Fatal("expected a non-pay response to be refused")
	}
}
