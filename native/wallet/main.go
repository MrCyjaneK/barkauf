package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"gitlab.com/ark-bitcoin/bark-ffi-bindings/golang/bark"

	"barkauf/wallet/accel"
)

const (
	server  = "https://ark.second.tech"
	esplora = "https://mempool.second.tech/api"
)

type request struct {
	Op        string   `json:"op"`
	AmountSat uint64   `json:"amount_sat"`
	URI       string   `json:"uri"`
	Mnemonic  string   `json:"mnemonic"`
	Txid      string   `json:"txid"`
	MaxBid    uint64   `json:"max_bid"`
	Method    string   `json:"method"`
	VtxoIDs   []string `json:"vtxo_ids,omitempty"`
}

type response struct {
	Op               string        `json:"op,omitempty"`
	OK               bool          `json:"ok"`
	HasWallet        bool          `json:"has_wallet"`
	Created          bool          `json:"created,omitempty"`
	Mnemonic         string        `json:"mnemonic,omitempty"`
	Fingerprint      string        `json:"fingerprint,omitempty"`
	Ark              string        `json:"ark,omitempty"`
	Onchain          string        `json:"onchain,omitempty"`
	Bolt11           string        `json:"bolt11,omitempty"`
	BIP321           string        `json:"bip321,omitempty"`
	CanArk           bool          `json:"can_ark"`
	CanLightning     bool          `json:"can_lightning"`
	CanOnchain       bool          `json:"can_onchain"`
	CanAll           bool          `json:"can_all"`
	QRPNG            string        `json:"qr_png,omitempty"`
	SpendableSats    uint64        `json:"spendable_sats"`
	TotalSats        uint64        `json:"total_sats"`
	OnchainTotal     uint64        `json:"onchain_total"`
	OnchainConfirmed uint64        `json:"onchain_confirmed"`
	OnchainPending   uint64        `json:"onchain_pending"`
	OffchainTotal    uint64        `json:"offchain_total"`
	PendingSend      uint64        `json:"pending_send"`
	PendingInRound   uint64        `json:"pending_in_round"`
	PendingExit      uint64        `json:"pending_exit"`
	PendingBoard     uint64        `json:"pending_board"`
	ClaimableReceive uint64        `json:"claimable_receive"`
	History          []historyItem `json:"history"`
	PayResult        string        `json:"pay_result,omitempty"`
	Error            string        `json:"error,omitempty"`
	Vtxos            []vtxoItem    `json:"vtxos,omitempty"`
	TipHeight        uint32        `json:"tip_height,omitempty"`
	VtxoExitDelta    uint32        `json:"vtxo_exit_delta,omitempty"`
	MinBoard         uint64        `json:"min_board,omitempty"`
	HasBoardFee      bool          `json:"has_board_fee,omitempty"`
	BoardGross       uint64        `json:"board_gross,omitempty"`
	BoardFee         uint64        `json:"board_fee,omitempty"`
	BoardNet         uint64        `json:"board_net,omitempty"`
	BoardTxid        string        `json:"board_txid,omitempty"`
	BoardAmount      uint64        `json:"board_amount,omitempty"`
	HasPayFee        bool          `json:"has_pay_fee,omitempty"`
	PayMethod        string        `json:"pay_method,omitempty"`
	PayGross         uint64        `json:"pay_gross,omitempty"`
	PayFee           uint64        `json:"pay_fee,omitempty"`
	PayNet           uint64        `json:"pay_net,omitempty"`
	Lnurl            string        `json:"lnurl,omitempty"`
	LnurlFixed       bool          `json:"lnurl_fixed,omitempty"`
	LnurlAmount      uint64        `json:"lnurl_amount,omitempty"`
	Accelerator      *accel.View   `json:"accelerator,omitempty"`
	RefreshCount     int           `json:"refresh_count"`
	RefreshIDs       []string      `json:"refresh_ids,omitempty"`
	RefreshSats      uint64        `json:"refresh_sats"`
	RefreshHeight    uint32        `json:"refresh_height"`
	RefreshInRound   int           `json:"refresh_in_round"`
	RoundSummary     string        `json:"round_summary,omitempty"`
	HasRefreshFee    bool          `json:"has_refresh_fee,omitempty"`
	RefreshGross     uint64        `json:"refresh_gross,omitempty"`
	RefreshFee       uint64        `json:"refresh_fee,omitempty"`
	RefreshNet       uint64        `json:"refresh_net,omitempty"`
	RefreshScheduled bool          `json:"refresh_scheduled,omitempty"`
	RefreshDetail    string        `json:"refresh_detail,omitempty"`
}

type vtxoItem struct {
	ID           string `json:"id"`
	Amount       uint64 `json:"amount"`
	Expiry       uint32 `json:"expiry_height"`
	Kind         string `json:"kind"`
	State        string `json:"state"`
	ExitDepth    uint32 `json:"exit_depth"`
	NeedsRefresh bool   `json:"needs_refresh,omitempty"`
	InRound      bool   `json:"in_round,omitempty"`
	RoundLabel   string `json:"round_label,omitempty"`
}

type payDest struct {
	Destination string `json:"destination"`
	Amount      int64  `json:"amount"`
}

// Flat view of one Noah-style transaction: a bark movement, an on-chain
// wallet tx, or a movement with the matching chain tx folded in.
type historyItem struct {
	ID               string    `json:"id"`
	Status           string    `json:"status,omitempty"`
	Kind             string    `json:"kind,omitempty"`
	Type             string    `json:"type"`
	Rail             string    `json:"rail"`
	Label            string    `json:"label"`
	Amount           int64     `json:"amount"`
	Direction        string    `json:"direction"`
	Transfer         bool      `json:"transfer"`
	Canceled         bool      `json:"canceled"`
	CreatedAt        string    `json:"created_at,omitempty"`
	DateLabel        string    `json:"date_label,omitempty"`
	Destination      string    `json:"destination,omitempty"`
	Txid             string    `json:"txid,omitempty"`
	HasOffchainFee   bool      `json:"has_offchain_fee"`
	OffchainFee      uint64    `json:"offchain_fee"`
	HasOnchainFee    bool      `json:"has_onchain_fee"`
	OnchainFee       uint64    `json:"onchain_fee"`
	Subsystem        string    `json:"subsystem,omitempty"`
	SubsystemKind    string    `json:"subsystem_kind,omitempty"`
	Height           uint32    `json:"height,omitempty"`
	BlockHash        string    `json:"block_hash,omitempty"`
	Source           string    `json:"source"`
	Intended         int64     `json:"intended"`
	Effective        int64     `json:"effective"`
	MovementID       uint32    `json:"movement_id,omitempty"`
	SortTime         int64     `json:"sort_time"`
	SortHeight       uint32    `json:"sort_height,omitempty"`
	Confirmed        bool      `json:"confirmed"`
	ChainAnchor      string    `json:"chain_anchor,omitempty"`
	HasBalanceChange bool      `json:"has_balance_change"`
	BalanceChange    int64     `json:"balance_change"`
	SentTo           []payDest `json:"sent_to,omitempty"`
	ReceivedOn       []payDest `json:"received_on,omitempty"`
}

type walletState struct {
	created          bool
	mnemonic         string
	fingerprint      string
	ark              string
	onchain          string
	bolt11           string
	amountSat        uint64
	spendable        uint64
	total            uint64
	onchainTotal     uint64
	onchainConfirmed uint64
	onchainPending   uint64
	offchainTotal    uint64
	pendingSend      uint64
	pendingInRound   uint64
	pendingExit      uint64
	pendingBoard     uint64
	claimableReceive uint64
	history          []historyItem
	datadir          string
	wallet           *bark.Wallet
	chain            *bark.OnchainWallet
	lastAccel        accel.View
	accel            *accel.Client
	refreshIDs       []string
	refreshSats      uint64
	refreshHeight    uint32
	refreshInRound   int
	roundSummary     string
	inRound          map[string]bool
	roundByVtxo      map[string]string
	roundInputs      []vtxoItem
	tipHeight        uint32
}

func main() {
	datadir := flag.String("datadir", "", "wallet data directory")
	flag.Parse()
	if *datadir == "" {
		fmt.Fprintln(os.Stderr, "datadir is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*datadir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var st *walletState
	var bootErr string
	mnemonic, hasFile, err := readMnemonic(*datadir)
	if err != nil {
		bootErr = err.Error()
	} else if hasFile {
		progress("Opening wallet")
		st, err = openWallet(*datadir, mnemonic, false)
		if err != nil {
			st = nil
			if uninitialised(err) {
				// The phrase was saved before Bark created the wallet.
				os.Remove(mnemonicPath(*datadir))
				os.RemoveAll(filepath.Join(*datadir, "onchain"))
				os.RemoveAll(filepath.Join(*datadir, "ark"))
				hasFile = false
			} else {
				bootErr = err.Error()
			}
		}
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeResp(response{Op: req.Op, OK: false, Error: err.Error()})
			continue
		}
		switch req.Op {
		case "generate":
			writeResp(generateMnemonic(st != nil))
		case "import":
			if st != nil || (hasFile && bootErr != "") {
				writeResp(response{Op: req.Op, OK: false, HasWallet: st != nil, Error: "wallet already exists"})
				continue
			}
			st, err = initNew(*datadir, req.Mnemonic)
			if err != nil {
				st = nil
				writeResp(response{Op: req.Op, OK: false, Error: err.Error()})
				continue
			}
			hasFile = true
			resp := st.snapshot("")
			resp.Op = req.Op
			writeResp(resp)
		default:
			if st == nil {
				writeResp(response{
					Op:        req.Op,
					OK:        bootErr == "",
					HasWallet: false,
					Error:     bootErr,
				})
				continue
			}
			resp := st.handle(req)
			resp.Op = req.Op
			writeResp(resp)
		}
	}
	if watchCancel != nil {
		watchCancel()
	}
	if watchDone != nil {
		select {
		case <-watchDone:
		case <-time.After(500 * time.Millisecond):
		}
	}
	if st != nil {
		st.wallet.Destroy()
		st.chain.Destroy()
	}
	os.Exit(0)
}

func mainnetConfig() bark.Config {
	esploraURL := esplora
	return bark.Config{
		ServerAddress:  server,
		EsploraAddress: &esploraURL,
	}
}

func mnemonicPath(datadir string) string {
	return filepath.Join(datadir, "mnemonic")
}

func normalizeMnemonic(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func uninitialised(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not initialised")
}

func readMnemonic(datadir string) (string, bool, error) {
	raw, err := os.ReadFile(mnemonicPath(datadir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	mnemonic := normalizeMnemonic(string(raw))
	if mnemonic == "" {
		return "", false, nil
	}
	return mnemonic, true, nil
}

func generateMnemonic(hasWallet bool) response {
	if hasWallet {
		return response{Op: "generate", OK: false, HasWallet: true, Error: "wallet already exists"}
	}
	mnemonic, err := bark.GenerateMnemonic()
	if err != nil {
		return response{Op: "generate", OK: false, Error: err.Error()}
	}
	return response{Op: "generate", OK: true, Mnemonic: mnemonic}
}

func initNew(datadir, mnemonic string) (*walletState, error) {
	mnemonic = normalizeMnemonic(mnemonic)
	if mnemonic == "" {
		return nil, fmt.Errorf("recovery phrase is empty")
	}
	ok, err := bark.ValidateMnemonic(mnemonic)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("recovery phrase is not valid")
	}
	path := mnemonicPath(datadir)
	if err := os.WriteFile(path, []byte(mnemonic+"\n"), 0600); err != nil {
		return nil, err
	}
	st, err := openWallet(datadir, mnemonic, true)
	if err != nil {
		os.Remove(path)
		os.RemoveAll(filepath.Join(datadir, "onchain"))
		os.RemoveAll(filepath.Join(datadir, "ark"))
		return nil, err
	}
	return st, nil
}

func progress(msg string) {
	fmt.Fprintln(os.Stderr, "progress:", msg)
}

func openWallet(datadir, mnemonic string, create bool) (*walletState, error) {
	cfg := mainnetConfig()
	chainDir := filepath.Join(datadir, "onchain")
	arkDir := filepath.Join(datadir, "ark")
	if err := os.MkdirAll(chainDir, 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(arkDir, 0700); err != nil {
		return nil, err
	}

	progress("Opening on-chain wallet")
	chain, err := bark.OnchainWalletDefault(bark.NetworkBitcoin, mnemonic, cfg, chainDir)
	if err != nil {
		return nil, err
	}
	progress("Connecting to Ark")
	w, err := bark.WalletOpen(bark.NetworkBitcoin, mnemonic, cfg, bark.WalletOpenArgs{
		Datadir:           arkDir,
		Onchain:           &chain,
		CreateIfNotExists: create,
	})
	if err != nil {
		chain.Destroy()
		return nil, err
	}

	progress("Creating addresses")
	arkAddr, err := w.NewAddress()
	if err != nil {
		w.Destroy()
		chain.Destroy()
		return nil, err
	}
	onchainAddr, err := chain.NewAddress()
	if err != nil {
		w.Destroy()
		chain.Destroy()
		return nil, err
	}

	st := &walletState{
		created:     create,
		mnemonic:    mnemonic,
		fingerprint: w.Fingerprint(),
		ark:         arkAddr,
		onchain:     onchainAddr,
		datadir:     datadir,
		history:     []historyItem{},
		wallet:      w,
		chain:       chain,
	}
	if err := st.refreshBolt11(); err != nil {
		fmt.Fprintln(os.Stderr, "bolt11:", err)
	}
	watchMovements(w)
	return st, nil
}

var watchCancel context.CancelFunc
var watchDone <-chan struct{}

func watchMovements(w *bark.Wallet) {
	if watchCancel != nil {
		watchCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	watchCancel = cancel
	ch, err := bark.NotificationChan(ctx, w)
	if err != nil {
		fmt.Fprintln(os.Stderr, "watch:", err)
		watchDone = nil
		return
	}
	done := make(chan struct{})
	watchDone = done
	go func() {
		defer close(done)
		for event := range ch {
			switch event.(type) {
			case bark.WalletNotificationMovementCreated, bark.WalletNotificationMovementUpdated:
				fmt.Fprintln(os.Stderr, "event:movement")
			}
			event.Destroy()
		}
	}()
}

func (st *walletState) refreshBolt11() error {
	st.bolt11 = ""
	if st.amountSat == 0 {
		return nil
	}
	inv, err := st.wallet.Bolt11Invoice(st.amountSat, nil, nil)
	if err != nil {
		return err
	}
	st.bolt11 = inv.Invoice
	return nil
}

func (st *walletState) uri() string {
	return bip321URI(st.ark, st.onchain, st.bolt11, st.amountSat)
}

func qrcodeEncode(text string, size int) (string, error) {
	png, err := qrcode.Encode(text, qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

func qrPNG(text string) string {
	if text == "" {
		return ""
	}
	png, err := qrcodeEncode(text, 256)
	if err != nil {
		return ""
	}
	return png
}

func (st *walletState) snapshot(pay string) response {
	return response{
		OK:               true,
		HasWallet:        true,
		Created:          st.created,
		Mnemonic:         st.mnemonic,
		Fingerprint:      st.fingerprint,
		Ark:              st.ark,
		Onchain:          st.onchain,
		Bolt11:           st.bolt11,
		BIP321:           st.uri(),
		CanArk:           st.ark != "",
		CanLightning:     st.bolt11 != "",
		CanOnchain:       st.onchain != "",
		CanAll:           st.ark != "" && st.bolt11 != "" && st.onchain != "",
		QRPNG:            qrPNG(st.uri()),
		SpendableSats:    st.spendable,
		TotalSats:        st.total,
		OnchainTotal:     st.onchainTotal,
		OnchainConfirmed: st.onchainConfirmed,
		OnchainPending:   st.onchainPending,
		OffchainTotal:    st.offchainTotal,
		PendingSend:      st.pendingSend,
		PendingInRound:   st.pendingInRound,
		PendingExit:      st.pendingExit,
		PendingBoard:     st.pendingBoard,
		ClaimableReceive: st.claimableReceive,
		History:          st.history,
		PayResult:        pay,
		RefreshCount:     len(st.refreshIDs),
		RefreshIDs:       st.refreshIDs,
		RefreshSats:      st.refreshSats,
		RefreshHeight:    st.refreshHeight,
		RefreshInRound:   st.refreshInRound,
		RoundSummary:     st.roundSummary,
		TipHeight:        st.tipHeight,
	}
}

func (st *walletState) refreshState() error {
	marker := filepath.Join(st.datadir, "onchain-scanned")
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		// sync only sees addresses this install already revealed.
		progress("Scanning chain history")
		if _, err := st.chain.InitialScan(nil); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte("1\n"), 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	progress("Syncing on-chain")
	if _, err := st.chain.Sync(); err != nil {
		return err
	}
	progress("Syncing Ark")
	if err := st.wallet.Sync(); err != nil {
		return err
	}
	progress("Updating rounds")
	if err := st.wallet.ProgressPendingRounds(); err != nil {
		fmt.Fprintln(os.Stderr, "progress rounds:", err)
	} else if err := st.wallet.Sync(); err != nil {
		fmt.Fprintln(os.Stderr, "sync after rounds:", err)
	}
	onchain, err := st.chain.Balance()
	if err != nil {
		return err
	}
	offchain, err := st.wallet.Balance()
	if err != nil {
		return err
	}
	progress("Loading activity")
	movements, err := st.wallet.History()
	if err != nil {
		return err
	}
	chainTxs, err := st.chain.Transactions()
	if err != nil {
		return err
	}

	st.onchainConfirmed = onchain.ConfirmedSats
	st.onchainPending = onchain.PendingSats
	st.onchainTotal = onchain.TotalSats
	if st.onchainTotal == 0 {
		st.onchainTotal = st.onchainConfirmed + st.onchainPending
	}
	st.spendable = offchain.SpendableSats
	st.pendingSend = offchain.PendingLightningSendSats
	st.pendingInRound = offchain.PendingInRoundSats
	st.pendingExit = offchain.PendingExitSats
	st.pendingBoard = offchain.PendingBoardSats
	st.claimableReceive = offchain.ClaimableLightningReceiveSats
	st.offchainTotal = st.spendable + st.pendingSend + st.pendingInRound + st.pendingExit + st.pendingBoard + st.claimableReceive
	st.total = st.onchainTotal + st.offchainTotal

	items := buildHistory(movements, chainTxs)
	progress(fmt.Sprintf("Loaded %d payments", len(items)))
	st.history = items
	if err := st.loadRefreshDue(); err != nil {
		fmt.Fprintln(os.Stderr, "vtxos to refresh:", err)
	}
	return nil
}

func buildHistory(movements []bark.Movement, chainTxs []bark.WalletTransaction) []historyItem {
	fromMovements := make([]historyItem, 0, len(movements))
	for _, m := range movements {
		if item, ok := movementItem(m); ok {
			fromMovements = append(fromMovements, item)
		}
	}
	fromChain := make([]historyItem, 0, len(chainTxs))
	for _, tx := range chainTxs {
		if tx.BalanceChangeSats == 0 {
			continue
		}
		fromChain = append(fromChain, onchainItem(tx))
	}
	seenHash := map[string]bool{}
	hashes := make([]string, 0)
	for _, item := range fromChain {
		if item.BlockHash == "" || seenHash[item.BlockHash] {
			continue
		}
		seenHash[item.BlockHash] = true
		hashes = append(hashes, item.BlockHash)
	}
	if len(hashes) > 0 {
		progress("Reading chain dates")
		times := fetchBlockTimestamps(hashes)
		for i := range fromChain {
			sec, ok := times[fromChain[i].BlockHash]
			if !ok || sec <= 0 {
				continue
			}
			when := time.Unix(sec, 0).UTC()
			fromChain[i].CreatedAt = when.Format(time.RFC3339)
			fromChain[i].DateLabel = ""
			fromChain[i].SortTime = when.UnixMilli()
		}
	}
	return mergeHistory(fromMovements, fromChain)
}

func movementItem(m bark.Movement) (historyItem, bool) {
	name := strings.ToLower(m.SubsystemName)
	kind := strings.ToLower(m.SubsystemKind)
	id := name + ":" + kind
	moveKind := movementKind(id)
	boarding := moveKind == "onboard" || moveKind == "offboard"
	if !boarding {
		if m.Status == "failed" {
			return historyItem{}, false
		}
		if m.IntendedBalanceSats == 0 && m.EffectiveBalanceSats == 0 {
			return historyItem{}, false
		}
	}
	outgoing := movementOutgoing(id, moveKind, m.EffectiveBalanceSats)
	amount := movementAmount(m, boarding)
	meta := parseMovementMeta(m.MetadataJson)
	txid := movementTxid(meta, m, outgoing)
	dests := m.ReceivedOnAddresses
	if outgoing {
		dests = m.SentToAddresses
	}
	destination := ""
	if len(dests) > 0 {
		destination = dests[0]
	}
	if destination == "" && m.LightningInvoice != nil {
		destination = *m.LightningInvoice
	}
	payType := movementType(id, moveKind, outgoing, dests)
	when, sortTime := movementWhen(m)
	item := historyItem{
		ID:             fmt.Sprintf("movement-%d", m.Id),
		Status:         m.Status,
		Kind:           moveKind,
		Type:           payType,
		Rail:           movementRail(moveKind, payType),
		Label:          movementLabel(moveKind, payType),
		Amount:         amount,
		Direction:      "incoming",
		Transfer:       moveKind == "onboard",
		Canceled:       m.Status == "canceled",
		CreatedAt:      when,
		Destination:    destination,
		Txid:           txid,
		HasOffchainFee: true,
		OffchainFee:    m.OffchainFeeSats,
		Subsystem:      m.SubsystemName,
		SubsystemKind:  m.SubsystemKind,
		Source:         "ark",
		Intended:       m.IntendedBalanceSats,
		Effective:      m.EffectiveBalanceSats,
		MovementID:     m.Id,
		SortTime:       sortTime,
		ChainAnchor:    meta.ChainAnchor,
		SentTo:         destList(m.SentToAddresses),
		ReceivedOn:     destList(m.ReceivedOnAddresses),
	}
	if outgoing {
		item.Direction = "outgoing"
	}
	if meta.OnchainFee != nil && *meta.OnchainFee >= 0 {
		item.HasOnchainFee = true
		item.OnchainFee = uint64(*meta.OnchainFee)
	}
	return item, true
}

func onchainItem(tx bark.WalletTransaction) historyItem {
	item := historyItem{
		ID:               "onchain-wallet-" + tx.Txid,
		Type:             "Onchain",
		Rail:             "onchain",
		Label:            "Onchain",
		Amount:           abs64(tx.BalanceChangeSats),
		Direction:        "incoming",
		CreatedAt:        "",
		DateLabel:        "Unconfirmed",
		Txid:             tx.Txid,
		Source:           "onchain",
		HasBalanceChange: true,
		BalanceChange:    tx.BalanceChangeSats,
	}
	if tx.BalanceChangeSats < 0 {
		item.Direction = "outgoing"
	}
	if tx.OnchainFeeSats != nil {
		item.HasOnchainFee = true
		item.OnchainFee = *tx.OnchainFeeSats
	}
	if tx.Confirmation != nil {
		item.Confirmed = true
		item.Height = tx.Confirmation.Height
		item.BlockHash = tx.Confirmation.Hash
		item.SortHeight = tx.Confirmation.Height
		item.DateLabel = ""
	}
	return item
}

func mergeHistory(movements, chain []historyItem) []historyItem {
	byTxid := map[string]historyItem{}
	for _, tx := range chain {
		if tx.Txid != "" {
			byTxid[tx.Txid] = tx
		}
	}
	used := map[string]bool{}
	for i, item := range movements {
		if (item.Kind != "onboard" && item.Kind != "offboard") || item.Txid == "" {
			continue
		}
		chainTx, ok := byTxid[item.Txid]
		if !ok {
			continue
		}
		used[chainTx.ID] = true
		if !item.HasOnchainFee && chainTx.HasOnchainFee {
			item.HasOnchainFee = true
			item.OnchainFee = chainTx.OnchainFee
		}
		item.HasBalanceChange = true
		item.BalanceChange = chainTx.BalanceChange
		item.Confirmed = chainTx.Confirmed
		item.Height = chainTx.Height
		item.BlockHash = chainTx.BlockHash
		item.SortHeight = chainTx.SortHeight
		movements[i] = item
	}
	out := make([]historyItem, 0, len(movements)+len(chain))
	out = append(out, movements...)
	for _, tx := range chain {
		if !used[tx.ID] {
			out = append(out, tx)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return historyBefore(out[i], out[j])
	})
	return out
}

func historyBefore(a, b historyItem) bool {
	aUnconfirmedChain := a.Source == "onchain" && !a.Confirmed
	bUnconfirmedChain := b.Source == "onchain" && !b.Confirmed
	if aUnconfirmedChain != bUnconfirmedChain {
		return aUnconfirmedChain
	}
	if a.SortTime > 0 && b.SortTime > 0 {
		return a.SortTime > b.SortTime
	}
	if a.Source == "onchain" && b.Source == "onchain" {
		return a.SortHeight > b.SortHeight
	}
	if (a.SortTime > 0) != (b.SortTime > 0) {
		return a.SortTime > 0
	}
	return a.SortTime > b.SortTime
}

func movementKind(id string) string {
	switch id {
	case "bark.board:board":
		return "onboard"
	case "bark.offboard:offboard", "bark.round:offboard":
		return "offboard"
	case "bark.offboard:send_onchain", "bark.round:send_onchain":
		return "send-onchain"
	case "bark.arkoor:receive":
		return "arkoor-receive"
	case "bark.exit:start":
		return "exit"
	case "bark.lightning_receive:receive":
		return "lightning-receive"
	default:
		return ""
	}
}

func movementOutgoing(id, moveKind string, effective int64) bool {
	switch id {
	case "bark.offboard:offboard", "bark.offboard:send_onchain",
		"bark.round:offboard", "bark.round:send_onchain",
		"bark.arkoor:send", "bark.lightning_send:send", "bark.exit:start":
		return true
	}
	if id == "" {
		return effective < 0
	}
	if moveKind == "arkoor-receive" || moveKind == "onboard" || moveKind == "lightning-receive" {
		return false
	}
	return effective < 0
}

func movementAmount(m bark.Movement, boarding bool) int64 {
	if boarding {
		if m.IntendedBalanceSats != 0 {
			return abs64(m.IntendedBalanceSats)
		}
		return abs64(m.EffectiveBalanceSats)
	}
	return abs64(m.EffectiveBalanceSats)
}

func movementType(id, moveKind string, outgoing bool, dests []string) string {
	if moveKind == "lightning-receive" || id == "bark.lightning_send:send" {
		return "Bolt11"
	}
	if hasBitcoinAddress(dests) {
		return "Onchain"
	}
	if moveKind == "arkoor-receive" || id == "bark.arkoor:send" {
		return "Arkoor"
	}
	if moveKind == "offboard" || moveKind == "onboard" || moveKind == "send-onchain" || moveKind == "exit" {
		return "Onchain"
	}
	if outgoing && len(dests) > 0 {
		return "Arkoor"
	}
	return "Onchain"
}

func movementLabel(moveKind, payType string) string {
	switch moveKind {
	case "onboard":
		return "Board"
	case "offboard":
		return "Offboard"
	case "exit":
		return "Ark Exit"
	}
	switch payType {
	case "Bolt11", "Lnurl":
		return "Lightning"
	case "Arkoor":
		return "Ark"
	default:
		return "Onchain"
	}
}

func movementRail(moveKind, payType string) string {
	switch moveKind {
	case "onboard":
		return "board"
	case "offboard":
		return "offboard"
	}
	switch payType {
	case "Bolt11", "Lnurl":
		return "lightning"
	case "Arkoor":
		return "ark"
	default:
		return "onchain"
	}
}

func movementWhen(m bark.Movement) (string, int64) {
	raw := m.CreatedAt
	if m.CompletedAt != nil && *m.CompletedAt != "" {
		raw = *m.CompletedAt
	}
	if raw == "" {
		return "", 0
	}
	if iso, ms, ok := parseBarkTime(raw); ok {
		return iso, ms
	}
	if !strings.HasSuffix(raw, "Z") {
		if iso, ms, ok := parseBarkTime(raw + "Z"); ok {
			return iso, ms
		}
	}
	return raw, 0
}

func parseBarkTime(raw string) (string, int64, bool) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		return t.UTC().Format(time.RFC3339), t.UnixMilli(), true
	}
	return "", 0, false
}

type movementMeta struct {
	OffboardTxid string   `json:"offboard_txid"`
	OnchainFee   *float64 `json:"onchain_fee_sat"`
	ChainAnchor  string   `json:"chain_anchor"`
}

func parseMovementMeta(raw string) movementMeta {
	var meta movementMeta
	if raw == "" {
		return meta
	}
	_ = json.Unmarshal([]byte(raw), &meta)
	return meta
}

func movementTxid(meta movementMeta, m bark.Movement, outgoing bool) string {
	txid := meta.OffboardTxid
	if txid == "" {
		txid = chainAnchorTxid(meta.ChainAnchor)
	}
	if txid != "" {
		return txid
	}
	ids := m.OutputVtxoIds
	if outgoing {
		ids = append(append([]string{}, m.InputVtxoIds...), m.OutputVtxoIds...)
		ids = append(ids, m.ExitedVtxoIds...)
	} else {
		ids = append(append([]string{}, m.OutputVtxoIds...), m.InputVtxoIds...)
		ids = append(ids, m.ExitedVtxoIds...)
	}
	for _, id := range ids {
		if id != "" {
			return id
		}
	}
	return fmt.Sprintf("movement-%d", m.Id)
}

func chainAnchorTxid(anchor string) string {
	i := strings.LastIndex(anchor, ":")
	if i <= 0 {
		return anchor
	}
	for _, c := range anchor[i+1:] {
		if c < '0' || c > '9' {
			return anchor
		}
	}
	return anchor[:i]
}

func destList(addrs []string) []payDest {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]payDest, 0, len(addrs))
	for _, addr := range addrs {
		if addr == "" {
			continue
		}
		out = append(out, payDest{Destination: addr})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasBitcoinAddress(addrs []string) bool {
	for _, addr := range addrs {
		if looksLikeBitcoinAddress(addr) {
			return true
		}
	}
	return false
}

func looksLikeBitcoinAddress(addr string) bool {
	if len(addr) < 26 || len(addr) > 90 {
		return false
	}
	if strings.HasPrefix(addr, "bc1") || strings.HasPrefix(addr, "BC1") {
		return true
	}
	switch addr[0] {
	case '1', '3':
		return true
	default:
		return false
	}
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func fetchBlockTimestamps(hashes []string) map[string]int64 {
	out := map[string]int64{}
	if len(hashes) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	client := &http.Client{Timeout: 8 * time.Second}
	for _, hash := range hashes {
		wg.Add(1)
		sem <- struct{}{}
		go func(hash string) {
			defer wg.Done()
			defer func() { <-sem }()
			sec, ok := fetchBlockTimestamp(client, hash)
			if !ok {
				return
			}
			mu.Lock()
			out[hash] = sec
			mu.Unlock()
		}(hash)
	}
	wg.Wait()
	return out
}

func fetchBlockTimestamp(client *http.Client, hash string) (int64, bool) {
	resp, err := client.Get(esplora + "/block/" + hash)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return 0, false
	}
	var body struct {
		Timestamp int64 `json:"timestamp"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return 0, false
	}
	if body.Timestamp <= 0 {
		return 0, false
	}
	return body.Timestamp, true
}

func fetchTipHeight() uint32 {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(esplora + "/blocks/tip/height")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return 0
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32))
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

func roundFlowName(kind bark.RoundFlowKind) string {
	switch kind {
	case bark.RoundFlowKindDelegatedPending:
		return "scheduled"
	case bark.RoundFlowKindPending:
		return "waiting"
	case bark.RoundFlowKindOngoing:
		return "in progress"
	case bark.RoundFlowKindAwaitingConfirmations:
		return "confirming"
	case bark.RoundFlowKindFailed:
		return "failed"
	case bark.RoundFlowKindCanceled:
		return "canceled"
	default:
		return ""
	}
}

func explainRefreshError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "unusable") {
		return "The server rejected these VTXOs. They are already refreshing, locked, or no longer spendable."
	}
	return msg
}

type scheduledRefresh struct {
	RoundID uint32   `json:"round_id"`
	Height  uint32   `json:"height,omitempty"`
	IDs     []string `json:"ids"`
}

func scheduledPath(datadir string) string {
	return filepath.Join(datadir, "scheduled-refresh.json")
}

func (st *walletState) saveScheduled(roundID, height uint32, ids []string) {
	body, err := json.Marshal(scheduledRefresh{RoundID: roundID, Height: height, IDs: ids})
	if err != nil {
		fmt.Fprintln(os.Stderr, "save scheduled refresh:", err)
		return
	}
	if err := os.WriteFile(scheduledPath(st.datadir), body, 0600); err != nil {
		fmt.Fprintln(os.Stderr, "save scheduled refresh:", err)
	}
}

func (st *walletState) loadScheduled() (scheduledRefresh, bool) {
	body, err := os.ReadFile(scheduledPath(st.datadir))
	if err != nil {
		return scheduledRefresh{}, false
	}
	var saved scheduledRefresh
	if err := json.Unmarshal(body, &saved); err != nil || saved.RoundID == 0 || len(saved.IDs) == 0 {
		return scheduledRefresh{}, false
	}
	return saved, true
}

func (st *walletState) forgetScheduled() {
	os.Remove(scheduledPath(st.datadir))
}

func roundStatusWord(kind bark.RoundFlowKind) string {
	switch kind {
	case bark.RoundFlowKindDelegatedPending:
		return "scheduled"
	case bark.RoundFlowKindPending:
		return "pending"
	case bark.RoundFlowKindOngoing:
		return "ongoing"
	case bark.RoundFlowKindAwaitingConfirmations:
		return "unconfirmed"
	default:
		return roundFlowName(kind)
	}
}

// Noah keeps rounds that are still open, and only the newest delegated
// participation. Older "scheduled" rounds stay in Bark's list after they
// have already been replaced, which is what produced
// "Scheduled in round 3, Scheduled in round 4, ...".
func keepRound(kind bark.RoundFlowKind, id, newestDelegated uint32) bool {
	switch kind {
	case bark.RoundFlowKindFailed, bark.RoundFlowKindCanceled:
		return false
	case bark.RoundFlowKindDelegatedPending:
		return id == newestDelegated
	default:
		return true
	}
}

func (st *walletState) loadPendingRounds() {
	type openRound struct {
		id   uint32
		kind bark.RoundFlowKind
	}
	progress("Reading rounds")
	inputs, err := st.wallet.PendingRoundInputVtxos()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pending round vtxos:", err)
		inputs = nil
	}
	copied := make([]vtxoItem, 0, len(inputs))
	for i := range inputs {
		if inputs[i].Id != "" && vtxoStateName(inputs[i].State) != "Spent" {
			copied = append(copied, vtxoItem{
				ID:        inputs[i].Id,
				Amount:    inputs[i].AmountSats,
				Expiry:    inputs[i].ExpiryHeight,
				Kind:      inputs[i].Kind,
				State:     vtxoStateName(inputs[i].State),
				ExitDepth: inputs[i].ExitDepth,
				InRound:   true,
			})
		}
		inputs[i].Destroy()
	}
	var newestDelegated uint32
	var raw []openRound
	statesOK := true
	progress("Reading round status")
	rounds, err := st.wallet.PendingRoundStates()
	if err != nil {
		statesOK = false
		fmt.Fprintln(os.Stderr, "pending rounds:", err)
	} else {
		raw = make([]openRound, 0, len(rounds))
		for i := range rounds {
			if rounds[i].State == bark.RoundFlowKindDelegatedPending && rounds[i].Id > newestDelegated {
				newestDelegated = rounds[i].Id
			}
			raw = append(raw, openRound{id: rounds[i].Id, kind: rounds[i].State})
			rounds[i].Destroy()
		}
	}
	open := make([]openRound, 0, len(raw))
	for _, round := range raw {
		if keepRound(round.kind, round.id, newestDelegated) {
			open = append(open, round)
		}
	}
	var shown uint32
	for _, round := range open {
		if round.id >= shown {
			shown = round.id
		}
	}
	summary := ""
	switch len(open) {
	case 0:
	case 1:
		summary = fmt.Sprintf("Round #%d %s", open[0].id, roundStatusWord(open[0].kind))
	default:
		summary = fmt.Sprintf("%d rounds pending", len(open))
	}
	rowLabel := "In a round"
	if shown != 0 {
		rowLabel = fmt.Sprintf("In round #%d", shown)
	}
	set := map[string]bool{}
	labels := map[string]string{}
	for i := range copied {
		set[copied[i].ID] = true
		labels[copied[i].ID] = rowLabel
		copied[i].RoundLabel = rowLabel
	}
	if saved, ok := st.loadScheduled(); ok {
		if shown != 0 && saved.RoundID == shown {
			for _, id := range saved.IDs {
				if id == "" {
					continue
				}
				set[id] = true
				labels[id] = rowLabel
			}
		} else if statesOK {
			st.forgetScheduled()
		}
	}
	st.inRound = set
	st.roundByVtxo = labels
	st.roundInputs = copied
	st.refreshInRound = len(set)
	st.roundSummary = summary
}

func vtxoStateName(state bark.VtxoState) string {
	switch state.(type) {
	case bark.VtxoStateSpendable:
		return "Spendable"
	case bark.VtxoStateLocked:
		return "Locked"
	case bark.VtxoStateSpent:
		return "Spent"
	case bark.VtxoStateExited:
		return "Exited"
	default:
		return ""
	}
}

func (st *walletState) arkInfo() *bark.ArkInfo {
	return st.wallet.ArkInfo()
}

func (st *walletState) handle(req request) response {
	switch req.Op {
	case "", "status":
		return st.snapshot("")
	case "uri":
		st.amountSat = req.AmountSat
		if err := st.refreshBolt11(); err != nil {
			resp := st.snapshot("")
			resp.Error = err.Error()
			return resp
		}
		return st.snapshot("")
	case "sync":
		if err := st.refreshState(); err != nil {
			resp := st.snapshot("")
			resp.OK = false
			resp.Error = err.Error()
			return resp
		}
		return st.snapshot("")
	case "pay":
		result, err := st.pay(req.URI)
		if err != nil {
			resp := st.snapshot("")
			resp.OK = false
			resp.Error = err.Error()
			return resp
		}
		if err := st.refreshState(); err != nil {
			fmt.Fprintln(os.Stderr, "sync after pay:", err)
		}
		return st.snapshot(result)
	case "vtxos":
		return st.vtxos()
	case "refresh_due":
		return st.refreshDue()
	case "refresh_fee":
		return st.refreshFee(req.VtxoIDs)
	case "refresh_vtxos":
		return st.refreshVtxos(req.VtxoIDs)
	case "ark_info":
		return st.arkBoardInfo()
	case "board_fee":
		return st.boardFee(req.AmountSat)
	case "pay_fee":
		return st.payFee(req.Method, req.URI, req.AmountSat)
	case "lnurl":
		return st.resolveLnurl(req.URI)
	case "board":
		return st.board(req.AmountSat)
	case "accel_preview":
		return st.accelPreview(req.Txid)
	case "accel_invoice":
		return st.accelInvoice(req.Txid, req.MaxBid)
	default:
		return response{OK: false, Error: "unknown op " + req.Op}
	}
}

func (st *walletState) lockedVtxos() map[string]bool {
	progress("Checking VTXO locks")
	list, err := st.wallet.Vtxos()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vtxo locks:", err)
		return map[string]bool{}
	}
	locked := make(map[string]bool, len(list))
	total := len(list)
	for i := range list {
		if total > 25 && ((i+1)%25 == 0 || i+1 == total) {
			progress(fmt.Sprintf("Checking %d of %d VTXO locks", i+1, total))
		}
		if _, ok := list[i].State.(bark.VtxoStateLocked); ok && list[i].Id != "" {
			locked[list[i].Id] = true
		}
		list[i].Destroy()
	}
	return locked
}

func (st *walletState) loadRefreshDue() error {
	st.loadPendingRounds()
	if st.inRound == nil {
		st.inRound = map[string]bool{}
	}
	locked := st.lockedVtxos()
	progress("Checking VTXOs to refresh")
	list, err := st.wallet.GetVtxosToRefresh()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(list))
	var amount uint64
	for i := range list {
		id := list[i].Id
		if id != "" && !st.inRound[id] && !locked[id] {
			ids = append(ids, id)
			amount += list[i].AmountSats
		}
		list[i].Destroy()
	}
	st.refreshInRound = len(st.roundInputs)
	st.refreshIDs = ids
	st.refreshSats = amount
	if next, err := st.wallet.GetNextRequiredRefreshBlockheight(); err != nil {
		fmt.Fprintln(os.Stderr, "next refresh height:", err)
	} else if next != nil {
		st.refreshHeight = *next
	} else {
		st.refreshHeight = 0
	}
	if tip, err := st.chain.TipHeight(); err != nil {
		fmt.Fprintln(os.Stderr, "tip height:", err)
	} else if tip > 0 {
		st.tipHeight = tip
	}
	return nil
}

func (st *walletState) vtxos() response {
	progress("Loading VTXOs")
	list, err := st.wallet.Vtxos()
	if err != nil {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = err.Error()
		return resp
	}
	if err := st.loadRefreshDue(); err != nil {
		fmt.Fprintln(os.Stderr, "vtxos to refresh:", err)
	}
	due := make(map[string]bool, len(st.refreshIDs))
	for _, id := range st.refreshIDs {
		due[id] = true
	}
	items := make([]vtxoItem, 0, len(list))
	seen := map[string]bool{}
	inRoundCount := 0
	total := len(list)
	for i := range list {
		if total > 25 && ((i+1)%25 == 0 || i+1 == total) {
			progress(fmt.Sprintf("Reading %d of %d VTXOs", i+1, total))
		}
		state := vtxoStateName(list[i].State)
		id := list[i].Id
		if state == "Spent" {
			list[i].Destroy()
			continue
		}
		seen[id] = true
		inRound := st.inRound[id]
		label := ""
		if inRound {
			inRoundCount++
			label = st.roundByVtxo[id]
			if label == "" {
				label = "In a round"
			}
		}
		items = append(items, vtxoItem{
			ID:           id,
			Amount:       list[i].AmountSats,
			Expiry:       list[i].ExpiryHeight,
			Kind:         list[i].Kind,
			State:        state,
			ExitDepth:    list[i].ExitDepth,
			NeedsRefresh: due[id] && !inRound,
			InRound:      inRound,
			RoundLabel:   label,
		})
		list[i].Destroy()
	}
	for _, input := range st.roundInputs {
		if input.ID == "" || seen[input.ID] {
			continue
		}
		seen[input.ID] = true
		inRoundCount++
		if input.RoundLabel == "" {
			input.RoundLabel = "In a round"
		}
		input.InRound = true
		input.NeedsRefresh = false
		items = append(items, input)
	}
	st.refreshInRound = inRoundCount
	progress(fmt.Sprintf("Loaded %d VTXOs", len(items)))
	resp := st.snapshot("")
	resp.Vtxos = items
	if tip := fetchTipHeight(); tip > 0 {
		st.tipHeight = tip
		resp.TipHeight = tip
	}
	if info := st.arkInfo(); info != nil {
		resp.VtxoExitDelta = info.VtxoExitDelta
		resp.MinBoard = info.MinBoardAmountSats
		info.Destroy()
	}
	return resp
}

func (st *walletState) refreshDue() response {
	if err := st.loadRefreshDue(); err != nil {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = err.Error()
		return resp
	}
	return st.snapshot("")
}

func (st *walletState) refreshFee(ids []string) response {
	if len(ids) == 0 {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = "Select at least one VTXO."
		return resp
	}
	est, err := st.wallet.EstimateRefreshFee(ids)
	if err != nil {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = explainRefreshError(err)
		return resp
	}
	resp := st.snapshot("")
	resp.HasRefreshFee = true
	resp.RefreshGross = est.GrossAmountSats
	resp.RefreshFee = est.FeeSats
	resp.RefreshNet = est.NetAmountSats
	est.Destroy()
	return resp
}

func (st *walletState) spendableIDs() map[string]bool {
	list, err := st.wallet.Vtxos()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vtxos before refresh:", err)
		return nil
	}
	spendable := make(map[string]bool, len(list))
	for i := range list {
		if _, ok := list[i].State.(bark.VtxoStateSpendable); ok && list[i].Id != "" {
			if st.tipHeight == 0 || list[i].ExpiryHeight == 0 || list[i].ExpiryHeight > st.tipHeight {
				spendable[list[i].Id] = true
			}
		}
		list[i].Destroy()
	}
	return spendable
}

func (st *walletState) refreshVtxos(ids []string) response {
	if len(ids) == 0 {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = "Select at least one VTXO."
		return resp
	}
	st.loadPendingRounds()
	spendable := st.spendableIDs()
	usable := make([]string, 0, len(ids))
	skipped := 0
	for _, id := range ids {
		if st.inRound[id] || (spendable != nil && !spendable[id]) {
			skipped++
			continue
		}
		usable = append(usable, id)
	}
	if len(usable) == 0 {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = "Those VTXOs are already refreshing, locked, or expired, so they were not submitted again."
		return resp
	}
	round, err := st.wallet.RefreshVtxosDelegated(usable)
	if err != nil {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = explainRefreshError(err)
		return resp
	}
	scheduled := false
	detail := ""
	if round != nil {
		scheduled = true
		var height uint32
		if round.ScheduledHeight != nil {
			height = *round.ScheduledHeight
		}
		st.saveScheduled(round.Id, height, usable)
		if height > 0 {
			detail = fmt.Sprintf("Scheduled in round %d for block %d.", round.Id, height)
		} else {
			detail = fmt.Sprintf("Scheduled in round %d.", round.Id)
		}
		round.Destroy()
	}
	if skipped > 0 {
		if detail != "" {
			detail += " "
		}
		detail += fmt.Sprintf("Left out %d VTXOs that are already refreshing or cannot be spent.", skipped)
	}
	syncErr := ""
	if err := st.refreshState(); err != nil {
		syncErr = err.Error()
		fmt.Fprintln(os.Stderr, "sync after refresh:", err)
	}
	resp := st.snapshot("")
	resp.RefreshScheduled = scheduled
	resp.RefreshDetail = detail
	if syncErr != "" {
		if scheduled {
			resp.Error = "Wallet sync failed after scheduling the refresh: " + syncErr
		} else {
			resp.Error = syncErr
		}
	}
	return resp
}

func (st *walletState) arkBoardInfo() response {
	resp := st.snapshot("")
	info := st.arkInfo()
	if info == nil {
		resp.OK = false
		resp.Error = "Ark info is unavailable"
		return resp
	}
	resp.MinBoard = info.MinBoardAmountSats
	info.Destroy()
	return resp
}

func (st *walletState) boardFee(amount uint64) response {
	est, err := st.wallet.EstimateBoardFee(amount)
	if err != nil {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = err.Error()
		return resp
	}
	resp := st.snapshot("")
	resp.HasBoardFee = true
	resp.BoardGross = est.GrossAmountSats
	resp.BoardFee = est.FeeSats
	resp.BoardNet = est.NetAmountSats
	est.Destroy()
	if info := st.arkInfo(); info != nil {
		resp.MinBoard = info.MinBoardAmountSats
		info.Destroy()
	}
	return resp
}

func (st *walletState) payFee(method, dest string, amount uint64) response {
	fail := func(err error) response {
		resp := st.snapshot("")
		resp.OK = false
		resp.PayMethod = method
		if err != nil {
			resp.Error = err.Error()
		}
		return resp
	}
	if amount == 0 {
		return fail(fmt.Errorf("amount is missing"))
	}
	var est bark.FeeEstimate
	var err error
	switch method {
	case "ark":
		est, err = st.wallet.EstimateArkoorPaymentFee(amount)
	case "lightning":
		est, err = st.estimateLightningFee(dest, amount)
	case "onchain":
		if dest == "" {
			return fail(fmt.Errorf("on-chain address is missing"))
		}
		if st.spendsOnchainWallet(amount) {
			resp := st.snapshot("")
			resp.PayMethod = method
			return resp
		}
		est, err = st.wallet.EstimateSendOnchainFee(dest, amount)
	default:
		return fail(fmt.Errorf("unknown payment method"))
	}
	if err != nil {
		return fail(err)
	}
	resp := st.snapshot("")
	resp.HasPayFee = true
	resp.PayMethod = method
	resp.PayGross = est.GrossAmountSats
	resp.PayFee = est.FeeSats
	resp.PayNet = est.NetAmountSats
	est.Destroy()
	return resp
}

func (st *walletState) board(amount uint64) response {
	var pending bark.PendingBoard
	var err error
	if amount == 0 {
		pending, err = st.wallet.BoardAll()
	} else {
		pending, err = st.wallet.BoardAmount(amount)
	}
	if err != nil {
		resp := st.snapshot("")
		resp.OK = false
		resp.Error = err.Error()
		return resp
	}
	txid := pending.Txid
	boarded := pending.AmountSats
	pending.Destroy()
	if err := st.refreshState(); err != nil {
		resp := st.snapshot("")
		resp.BoardTxid = txid
		resp.BoardAmount = boarded
		resp.Error = err.Error()
		return resp
	}
	resp := st.snapshot("")
	resp.BoardTxid = txid
	resp.BoardAmount = boarded
	return resp
}

func (st *walletState) pay(raw string) (string, error) {
	parsed, err := parseBIP321(raw)
	if err != nil {
		return "", err
	}
	if parsed.lnaddr != "" {
		if !parsed.hasAmount || parsed.amountSat == 0 {
			return "", fmt.Errorf("Lightning address has no amount")
		}
		return st.payLightningAddress(parsed.lnaddr, parsed.amountSat)
	}
	if parsed.lnurl != "" {
		amount := parsed.amountSat
		if !parsed.hasAmount || amount == 0 {
			details, err := fetchLnurlLink(parsed.lnurl)
			if err != nil {
				return "", err
			}
			sats, ok := fixedLnurlSats(details)
			if !ok {
				return "", fmt.Errorf("Lightning link has no amount")
			}
			amount = sats
		}
		return st.payLnurl(parsed.lnurl, amount)
	}
	if parsed.lightning != "" {
		amt, err := lightningPayAmount(parsed.lightning, parsed.amountSat, parsed.hasAmount)
		if err != nil {
			return "", err
		}
		status, err := st.wallet.PayLightningInvoice(parsed.lightning, amt, true)
		if err != nil {
			return "", err
		}
		if err := settledLightning(status); err != nil {
			return "", err
		}
		return "paid", nil
	}
	if parsed.ark != "" {
		if !parsed.hasAmount || parsed.amountSat == 0 {
			return "", fmt.Errorf("Ark destination has no amount")
		}
		if err := st.wallet.SendArkoorPayment(parsed.ark, parsed.amountSat); err != nil {
			return "", err
		}
		return "sent", nil
	}
	if !parsed.hasAmount || parsed.amountSat == 0 {
		return "", fmt.Errorf("on-chain destination has no amount")
	}
	return st.payOnchain(parsed.onchain, parsed.amountSat)
}

// lightningPayAmount decides what to pass to PayLightningInvoice.
// An invoice that already encodes an amount must be paid at that amount.
// A URI amount is only used for an amountless invoice, and a mismatch is refused.
func lightningPayAmount(invoice string, uriAmount uint64, hasURI bool) (*uint64, error) {
	invoiceSats, hasInvoice := accel.InvoiceSats(invoice)
	if hasInvoice {
		if hasURI && uriAmount != invoiceSats {
			return nil, fmt.Errorf("Lightning invoice amount does not match the payment request")
		}
		return nil, nil
	}
	if !hasURI || uriAmount == 0 {
		return nil, fmt.Errorf("Lightning invoice has no amount")
	}
	amount := uriAmount
	return &amount, nil
}

func settledLightning(status bark.LightningSendStatus) error {
	defer status.Destroy()
	switch s := status.(type) {
	case bark.LightningSendStatusPaid:
		return nil
	case bark.LightningSendStatusInProgress:
		if s.Send.HasFailedRevocation {
			return fmt.Errorf("Lightning payment failed and the funds are still locked")
		}
		return fmt.Errorf("Lightning payment has not settled yet")
	default:
		return fmt.Errorf("Lightning payment did not settle")
	}
}

// spendsOnchainWallet is true when confirmed on-chain coins can fund the
// payment. The miner fee has to come out of the surplus, so a balance equal
// to the amount is left for the Ark send path.
func (st *walletState) spendsOnchainWallet(amount uint64) bool {
	return amount > 0 && st.onchainConfirmed > amount
}

func satVbFromKwu(satPerKwu uint64) uint64 {
	// 1 vbyte = 4 weight units and 1 kWU = 1000 WU, so sat/vB = sat/kWU / 250.
	rate := satPerKwu / 250
	if satPerKwu%250 != 0 {
		rate++
	}
	if rate == 0 {
		rate = 1
	}
	return rate
}

func (st *walletState) payOnchain(address string, amount uint64) (string, error) {
	if st.spendsOnchainWallet(amount) {
		rates, err := st.chain.FeeRates()
		if err != nil {
			return "", err
		}
		txid, err := st.chain.Send(address, amount, satVbFromKwu(rates.RegularSatPerKwu))
		if err != nil {
			return "", err
		}
		return txid, nil
	}
	if st.spendable < amount {
		return "", fmt.Errorf("not enough bitcoin to send this amount")
	}
	txid, err := st.wallet.SendOnchain(address, amount)
	if err != nil {
		return "", err
	}
	return txid, nil
}

func writeResp(resp response) {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
