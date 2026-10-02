package main

import (
	"strings"

	"barkauf/wallet/accel"
)

func (st *walletState) accelAPI() *accel.Client {
	if st != nil && st.accel != nil {
		return st.accel
	}
	return accel.Default
}

func (st *walletState) accelPreview(txid string) response {
	return st.accelResponse(st.accelAPI().Preview(txid))
}

func (st *walletState) accelInvoice(txid string, maxBid uint64) response {
	base := st.lastAccel
	id, err := accel.Normalize(txid)
	if err != nil {
		base.Txid = strings.TrimSpace(txid)
		return st.accelResponse(base.WithInvoice(accel.Invoice{}, err))
	}
	if !strings.EqualFold(base.Txid, id) {
		base = accel.Empty(id)
	}
	inv, err := st.accelAPI().CreateInvoice(id, maxBid)
	base = base.WithInvoice(inv, err)
	if err == nil {
		base.QRPNG = qrPNGSize(base.Invoice, 512)
	}
	return st.accelResponse(base)
}

func (st *walletState) accelResponse(view accel.View) response {
	st.lastAccel = view
	resp := st.snapshot("")
	copied := view
	resp.Accelerator = &copied
	return resp
}

func qrPNGSize(text string, size int) string {
	if text == "" {
		return ""
	}
	if size <= 0 {
		size = 256
	}
	png, err := qrcodeEncode(text, size)
	if err != nil {
		return ""
	}
	return png
}
