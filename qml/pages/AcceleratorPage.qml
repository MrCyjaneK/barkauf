import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page

    property string txid: ""
    property double selectedBid: 0
    property bool paying: false
    property bool paid: false
    property string failure: ""
    property bool copied: false
    property double nowSec: Date.now() / 1000

    property var quote: {
        var raw = Wallet.acceleratorJson
        var parsed = {}
        try {
            parsed = JSON.parse(raw || "{}")
        } catch (e) {
            parsed = {}
        }
        if (String(parsed.txid || "") !== page.txid)
            return {}
        return parsed
    }

    function sats(n) {
        return SendParse.group(n) + " sats"
    }
    function namesOf(list) {
        if (!list || !list.length)
            return ""
        var shown = []
        var limit = Math.min(list.length, 4)
        for (var i = 0; i < limit; ++i)
            shown.push(String(list[i]))
        var text = shown.join(", ")
        if (list.length > limit)
            text += " +" + (list.length - limit)
        return text
    }
    function when(sec) {
        var n = Number(sec) || 0
        if (n <= 0)
            return ""
        return Qt.formatDateTime(new Date(n * 1000), "d MMM yyyy HH:mm")
    }
    function options() {
        var list = page.quote.options || []
        return list
    }
    function selectedFee() {
        var fee = Number(page.selectedBid) || 0
        if (fee > 0)
            return fee
        var list = page.options()
        return list.length ? (Number(list[0]) || 0) : 0
    }
    function boostRate(fee) {
        var vsize = Number(page.quote.effective_vsize) || 0
        if (!(vsize > 0))
            return ""
        return "~ " + SendParse.satVb((Number(page.quote.effective_fee) + Number(fee || 0)) / vsize)
    }
    function serviceFee() {
        return Number(page.quote.mempool_base_fee || 0) + Number(page.quote.vsize_fee || 0)
    }
    function estimated(fee) {
        return page.serviceFee() + Number(fee || 0)
    }
    function dueSats() {
        var n = Number(page.quote.due_sats || 0)
        if (n > 0)
            return n
        var fromInvoice = SendParse.bolt11Amount(page.quote.invoice || "")
        return fromInvoice > 0 ? fromInvoice : 0
    }
    function expired() {
        var exp = Number(page.quote.expires || 0)
        if (exp <= 0)
            return false
        return page.nowSec >= exp
    }
    function expiresLabel() {
        var exp = Number(page.quote.expires || 0)
        if (exp <= 0)
            return ""
        var left = exp - page.nowSec
        if (left <= 0)
            return "Expired"
        var mins = Math.floor(left / 60)
        if (mins < 1)
            return "Expires in less than a minute"
        if (mins < 180)
            return "Expires in " + mins + " min"
        return "Expires " + page.when(exp)
    }
    function canInvoice() {
        var a = page.quote
        if (!a.has_estimate || a.unavailable || !a.bitcoin_enabled)
            return false
        var fee = page.selectedFee()
        if (fee <= 0)
            return false
        var min = Number(a.bitcoin_min || 0)
        var max = Number(a.bitcoin_max || 0)
        if (min > 0 && fee < min)
            return false
        if (max > 0 && fee > max)
            return false
        return true
    }
    function canPay() {
        var due = page.dueSats()
        return due > 0 && Number(Wallet.spendableSats) >= due && !page.expired() && !page.paid
    }
    function shortfall() {
        var due = page.dueSats()
        var have = Number(Wallet.spendableSats)
        return due > have ? due - have : 0
    }
    function ready() {
        return !!(page.quote.has_estimate || page.quote.has_boost || page.quote.error
                  || page.quote.estimate_error || page.quote.has_invoice)
    }
    function ensure() {
        if (!page.txid.length || page.ready())
            return
        Wallet.accelPreview(page.txid)
    }
    function refresh() {
        if (!page.txid.length || Wallet.busy)
            return
        page.paid = false
        page.failure = ""
        page.paying = false
        Wallet.accelPreview(page.txid)
    }
    function createInvoice() {
        if (!page.canInvoice() || Wallet.busy)
            return
        page.failure = ""
        page.paid = false
        Wallet.accelInvoice(page.txid, page.selectedFee())
    }
    function askPay() {
        if (!page.canPay() || page.paying || Wallet.busy)
            return
        page.failure = ""
        var dialog = pageStack.push(Qt.resolvedUrl("PayConfirmDialog.qml"), {
            amountText: page.sats(page.dueSats()),
            methodName: "Lightning · mempool accelerator",
            destination: String(page.quote.invoice || "")
        })
        dialog.accepted.connect(function () { page.startPay() })
    }
    function startPay() {
        if (page.paying || Wallet.busy || !page.canPay())
            return
        page.failure = ""
        var parsed = { lightning: String(page.quote.invoice || "") }
        Wallet.pay(SendParse.payUri("lightning", parsed, page.dueSats()))
        if (!Wallet.busy) {
            page.failure = Wallet.error.length ? Wallet.error : "Payment could not be started"
            return
        }
        page.paying = true
    }
    function finishPay() {
        if (!page.paying || Wallet.lastOp !== "pay")
            return
        page.paying = false
        if (Wallet.error.length)
            page.failure = Wallet.error
        else
            page.paid = true
    }
    function copyInvoice() {
        var text = String(page.quote.invoice || "")
        if (!text.length)
            return
        Clipboard.text = text
        page.copied = true
        copyTimer.restart()
    }

    Timer {
        id: copyTimer
        interval: 1200
        onTriggered: page.copied = false
    }
    Timer {
        interval: 15000
        repeat: true
        running: page.quote.has_invoice === true
        onTriggered: page.nowSec = Date.now() / 1000
    }

    onQuoteChanged: {
        var list = page.options()
        for (var i = 0; i < list.length; ++i) {
            if (Number(list[i]) === page.selectedBid)
                return
        }
        // mempool's own checkout starts on the middle bid, not the cheapest.
        var pick = list.length > 1 ? 1 : 0
        page.selectedBid = list.length ? (Number(list[pick]) || 0) : 0
    }

    Component.onCompleted: page.ensure()

    Connections {
        target: Wallet
        onChanged: page.finishPay()
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        PullDownMenu {
            MenuItem {
                text: "Refresh quote"
                enabled: !Wallet.busy
                onClicked: page.refresh()
            }
        }

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader {
                title: "Accelerate"
                description: Wallet.busy && (Wallet.lastOp === "accel_preview" || Wallet.lastOp === "accel_invoice" || page.paying)
                             ? Wallet.activity : "mempool.space"
            }

            Item {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                height: Math.max(mark.height, names.implicitHeight) + Theme.paddingLarge

                MempoolMark {
                    id: mark
                    width: Theme.iconSizeMedium
                    height: width
                    anchors.verticalCenter: parent.verticalCenter
                }
                Column {
                    id: names
                    anchors.left: mark.right
                    anchors.leftMargin: Theme.paddingLarge
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.paddingSmall
                    Label {
                        width: parent.width
                        text: "mempool"
                        font.bold: true
                        font.pixelSize: Theme.fontSizeLarge
                        truncationMode: TruncationMode.Fade
                    }
                    Label {
                        width: parent.width
                        color: Theme.secondaryColor
                        font.pixelSize: Theme.fontSizeSmall
                        text: "Public accelerator"
                        truncationMode: TruncationMode.Fade
                    }
                }
            }
            Rectangle {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                height: Math.max(2, Theme.paddingSmall / 2)
                radius: height / 2
                color: "#7c5cff"
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                text: "Speed up this unconfirmed onchain payment. mempool bids it to mining pools. You pay their Lightning invoice."
            }

            Column {
                width: parent.width
                visible: !page.ready()
                spacing: Theme.paddingLarge
                Item {
                    width: 1
                    height: Theme.paddingMedium
                }
                BusyIndicator {
                    anchors.horizontalCenter: parent.horizontalCenter
                    size: BusyIndicatorSize.Medium
                    running: parent.visible
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    color: Theme.secondaryColor
                    text: "Asking mempool for a quote…"
                }
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: String(page.quote.estimate_error || "").length > 0
                text: page.quote.estimate_error || ""
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: String(page.quote.error || "").length > 0 && !page.quote.has_invoice
                text: page.quote.error || ""
            }

            Column {
                width: parent.width
                visible: page.quote.has_boost === true
                spacing: Theme.paddingSmall
                SectionHeader { text: "Current boost" }
                DetailLine { label: "Status"; value: "Accelerating" }
                DetailLine { label: "Since"; value: page.when(page.quote.boost_added) }
                DetailLine {
                    label: "Added fee"
                    value: Number(page.quote.boost_fee_delta) > 0 ? page.sats(page.quote.boost_fee_delta) : ""
                }
                DetailLine {
                    label: "Effective fee"
                    value: Number(page.quote.boost_effective_fee) > 0
                           ? page.sats(page.quote.boost_effective_fee) + " · "
                             + SendParse.feeRate(page.quote.boost_effective_fee, page.quote.boost_effective_vsize)
                           : ""
                }
                DetailLine {
                    label: "Size"
                    value: Number(page.quote.boost_effective_vsize) > 0 ? String(page.quote.boost_effective_vsize) + " vB" : ""
                }
                DetailLine { label: "Pools"; value: page.namesOf(page.quote.boost_pool_names) }
            }

            Column {
                width: parent.width
                visible: page.quote.has_estimate === true
                spacing: Theme.paddingSmall
                SectionHeader { text: "Transaction" }
                DetailLine { label: "Transaction"; value: page.txid; copyable: true }
                DetailLine {
                    label: "Current fee"
                    value: page.sats(page.quote.effective_fee) + " · "
                           + SendParse.feeRate(page.quote.effective_fee, page.quote.effective_vsize)
                }
                DetailLine {
                    label: "Size"
                    value: Number(page.quote.effective_vsize) > 0 ? String(page.quote.effective_vsize) + " vB" : ""
                }
                DetailLine {
                    label: "Ancestors"
                    value: Number(page.quote.ancestor_count) > 1
                           ? String(page.quote.ancestor_count) + " in this package" : ""
                }
                DetailLine { label: "Target rate"; value: SendParse.satVb(page.quote.target_fee_rate) }
                DetailLine {
                    label: "Next block fee"
                    value: Number(page.quote.next_block_fee) > 0 ? page.sats(page.quote.next_block_fee) : ""
                }
                DetailLine {
                    label: "Service fee"
                    value: page.serviceFee() > 0 ? page.sats(page.serviceFee()) : ""
                }
                DetailLine {
                    label: "Credit"
                    value: Number(page.quote.user_balance) > 0 ? page.sats(page.quote.user_balance) : ""
                }
                DetailLine { label: "Pools"; value: page.namesOf(page.quote.pool_names) }
                DetailLine {
                    label: "Explorer"
                    value: "https://mempool.space/tx/" + page.txid
                    link: true
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.highlightColor
                    visible: page.quote.unavailable === true
                    text: "mempool cannot take a new acceleration for this transaction right now."
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.highlightColor
                    visible: page.quote.has_estimate === true && page.quote.bitcoin_enabled === false && page.quote.unavailable !== true
                    text: "A Lightning invoice is not available for this transaction."
                }
            }

            Column {
                width: parent.width
                visible: page.quote.has_estimate === true && page.quote.unavailable !== true
                         && page.quote.bitcoin_enabled !== false && page.options().length > 0
                         && !page.quote.has_invoice
                spacing: 0
                SectionHeader { text: "Choose a bid" }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.secondaryColor
                    visible: page.serviceFee() > 0
                    text: "The estimate adds mempool's service fee to the bid. The invoice shows the exact amount before you pay."
                }
                DetailLine {
                    label: "Accelerate to"
                    value: page.boostRate(page.selectedFee())
                }
                DetailLine {
                    label: "Bid"
                    value: page.selectedFee() > 0 ? page.sats(page.selectedFee()) : ""
                }
                DetailLine {
                    label: "You pay"
                    value: page.selectedFee() > 0 ? page.sats(page.estimated(page.selectedFee())) : ""
                }
                Repeater {
                    model: page.options()
                    delegate: BackgroundItem {
                        width: parent.width
                        height: Theme.itemSizeSmall
                        property bool chosen: Number(modelData) === page.selectedBid
                        highlighted: chosen
                        onClicked: page.selectedBid = Number(modelData)
                        Row {
                            x: Theme.horizontalPageMargin
                            width: parent.width - 2 * x
                            height: parent.height
                            Label {
                                width: parent.width * 0.62
                                anchors.verticalCenter: parent.verticalCenter
                                textFormat: Text.RichText
                                font.bold: chosen
                                color: chosen ? Theme.highlightColor : Theme.primaryColor
                                text: page.sats(page.estimated(modelData))
                            }
                            Label {
                                width: parent.width * 0.38
                                anchors.verticalCenter: parent.verticalCenter
                                horizontalAlignment: Text.AlignRight
                                color: chosen ? Theme.highlightColor : Theme.secondaryColor
                                font.pixelSize: Theme.fontSizeExtraSmall
                                textFormat: Text.RichText
                                text: chosen
                                      ? "Selected"
                                      : (page.serviceFee() > 0 ? ("Bid " + SendParse.group(modelData)) : "")
                            }
                        }
                    }
                }
                Item {
                    width: parent.width
                    height: createButton.height + Theme.paddingLarge
                    Button {
                        id: createButton
                        anchors.horizontalCenter: parent.horizontalCenter
                        anchors.bottom: parent.bottom
                        preferredWidth: Theme.buttonWidthLarge
                        text: Wallet.busy && Wallet.activity === "Creating accelerator invoice"
                              ? "Creating invoice…" : "Create invoice"
                        enabled: page.canInvoice() && !Wallet.busy
                        onClicked: page.createInvoice()
                    }
                }
            }

            Column {
                width: parent.width
                visible: page.quote.has_invoice === true
                spacing: Theme.paddingSmall

                SectionHeader { text: "Lightning invoice" }

                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    visible: page.paid
                    color: "#3dcc8a"
                    font.bold: true
                    font.pixelSize: Theme.fontSizeLarge
                    text: "Paid from Barkauf"
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    color: Theme.secondaryColor
                    visible: page.paid
                    text: "mempool picks up the acceleration after the Lightning payment settles."
                }

                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    font.pixelSize: Theme.fontSizeExtraLarge
                    font.bold: true
                    textFormat: Text.RichText
                    text: page.sats(page.dueSats())
                }
                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    color: Theme.secondaryColor
                    visible: String(page.quote.btc_due || "").length > 0
                    text: String(page.quote.btc_due) + " BTC"
                }
                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    color: page.expired() ? "#ff5c5c" : Theme.secondaryColor
                    text: page.expiresLabel()
                }

                Rectangle {
                    anchors.horizontalCenter: parent.horizontalCenter
                    width: Math.min(parent.width - 4 * Theme.horizontalPageMargin, Theme.itemSizeHuge * 3)
                    height: width
                    radius: Theme.paddingSmall
                    color: "white"
                    visible: Wallet.accelQrImage.length > 0
                    Image {
                        anchors.fill: parent
                        anchors.margins: Theme.paddingMedium
                        cache: false
                        fillMode: Image.PreserveAspectFit
                        source: Wallet.accelQrImage
                    }
                }

                DetailLine { label: "Invoice id"; value: page.quote.invoice_id || ""; copyable: true }
                DetailLine {
                    label: "Spendable"
                    value: page.sats(Wallet.spendableSats)
                }
                DetailLine {
                    label: "Still needed"
                    value: !page.paid && page.shortfall() > 0 ? page.sats(page.shortfall()) : ""
                }

                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    color: page.canPay() ? "#3dcc8a" : "#ff5c5c"
                    visible: !page.paid
                    text: page.expired()
                          ? "This invoice has expired. Refresh the quote and create another."
                          : (page.canPay()
                             ? "Spendable Ark bitcoin covers this invoice."
                             : "Not enough spendable bitcoin. Barkauf pays Lightning from Ark, so onchain funds are not used here. You can still pay the QR from another wallet.")
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    color: Theme.highlightColor
                    visible: page.failure.length > 0
                    text: page.failure
                }

                Item {
                    width: parent.width
                    height: payButton.height + Theme.paddingMedium
                    visible: !page.paid
                    Button {
                        id: payButton
                        anchors.horizontalCenter: parent.horizontalCenter
                        preferredWidth: Theme.buttonWidthLarge
                        text: page.paying ? "Paying…" : "Pay from Barkauf"
                        enabled: page.canPay() && !page.paying && !Wallet.busy
                        onClicked: page.askPay()
                    }
                }
                BackgroundItem {
                    width: parent.width
                    height: Theme.itemSizeSmall
                    onClicked: page.copyInvoice()
                    Label {
                        anchors.centerIn: parent
                        text: page.copied ? "Invoice copied" : "Copy invoice"
                        color: Theme.highlightColor
                    }
                }
            }

            Item { width: 1; height: Theme.paddingLarge }
        }
        VerticalScrollDecorator {}
    }
}
