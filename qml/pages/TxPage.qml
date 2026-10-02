import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property var entry: ({})
    property double rate: 0
    property bool more: false
    property bool copiedFlash: false

    ListModel { id: sentModel }
    ListModel { id: gotModel }

    Timer {
        id: copyTimer
        interval: 1200
        onTriggered: page.copiedFlash = false
    }

    Component.onCompleted: {
        fillDestinations(sentModel, destRows(entry.sent_to))
        fillDestinations(gotModel, destRows(entry.received_on))
    }

    onStatusChanged: {
        if (status === PageStatus.Active && page.boostTxid().length)
            boost.reload()
    }

    function fillDestinations(model, rows) {
        for (var i = 0; i < rows.length; ++i)
            model.append({ address: rows[i].address, amount: rows[i].amount })
    }

    function addressOf(item) {
        var guard = 0
        while (item && guard < 4) {
            guard++
            if (typeof item === "string") {
                var text = item
                if (!text.length)
                    return ""
                var head = text.charAt(0)
                if (head === "{" || head === "[") {
                    try {
                        item = JSON.parse(text)
                        continue
                    } catch (e) {
                        return text
                    }
                }
                return text
            }
            if (typeof item === "object") {
                if (item.length && item[0] && !item.destination && !item.address)
                    item = item[0]
                else if (item.destination)
                    item = item.destination
                else if (item.address)
                    item = item.address
                else if (item.value)
                    item = item.value
                else
                    return ""
                continue
            }
            return String(item)
        }
        return ""
    }
    function amountOf(item) {
        if (!item)
            return 0
        if (typeof item === "string" && (item.charAt(0) === "{" || item.charAt(0) === "[")) {
            try { item = JSON.parse(item) } catch (e) { return 0 }
        }
        if (item && typeof item === "object" && !item.length)
            return Number(item.amount || 0)
        return 0
    }
    function destRows(list) {
        if (typeof list === "string") {
            try { list = JSON.parse(list) } catch (e) { return [] }
        }
        if (!list || !list.length)
            return []
        var out = []
        for (var i = 0; i < list.length; ++i) {
            var addr = addressOf(list[i])
            if (!addr.length)
                continue
            out.push({ address: addr, amount: amountOf(list[i]) })
        }
        return out
    }
    function place() {
        var direct = addressOf(entry.destination)
        if (direct.length)
            return direct
        var rows = entry.direction === "outgoing" ? sentModel : gotModel
        if (rows.count === 0)
            rows = rows === sentModel ? gotModel : sentModel
        if (rows.count === 0)
            return ""
        return String(rows.get(0).address || "")
    }

    function magnitude() {
        return Math.abs(Number(entry.amount || 0))
    }
    function grouped(n) {
        var sign = ""
        var v = Number(n)
        if (v < 0) {
            sign = "−"
            v = -v
        } else if (v > 0) {
            sign = ""
        }
        return sign + SendParse.group(v) + " sats"
    }
    function actionLabel() {
        if (entry.canceled)
            return "Canceled"
        if (entry.transfer)
            return "Transferred"
        if (entry.direction === "outgoing")
            return "Sent"
        return "Received"
    }
    function whenText() {
        var label = String(entry.date_label || "")
        if (label.indexOf("Confirmed at block") === 0)
            label = ""
        if (label.length)
            return label
        var when = String(entry.created_at || "")
        if (!when.length)
            return ""
        var d = new Date(when)
        if (isNaN(d.getTime()))
            return when
        return Qt.formatDateTime(d, "d MMM yyyy HH:mm")
    }
    function statusLabel() {
        var s = String(entry.status || "")
        if (s === "successful" || (entry.source === "onchain" && entry.confirmed))
            return "Completed"
        if (s === "failed")
            return "Failed"
        if (s === "canceled")
            return "Canceled"
        if (s === "pending")
            return "Pending"
        if (entry.source === "onchain")
            return entry.confirmed ? "Confirmed" : "Unconfirmed"
        if (s.length)
            return s.charAt(0).toUpperCase() + s.slice(1)
        return "Recorded"
    }
    function statusColor() {
        var label = statusLabel()
        if (label === "Completed" || label === "Confirmed")
            return "#3dcc8a"
        if (label === "Failed" || label === "Canceled")
            return "#ff5c5c"
        return "#f7931a"
    }
    function kindLabel() {
        var kind = String(entry.kind || "")
        if (kind === "arkoor-receive")
            return "Ark Receive"
        if (kind === "onboard")
            return "Board"
        if (kind === "offboard")
            return "Offboard"
        if (kind === "send-onchain")
            return "Onchain Send"
        if (kind === "exit")
            return "Ark Exit"
        if (kind === "lightning-receive")
            return "Lightning Receive"
        return kind
    }
    function movementStatus() {
        var s = String(entry.status || "")
        if (s === "pending")
            return "Pending"
        if (s === "successful")
            return "Successful"
        if (s === "failed")
            return "Failed"
        if (s === "canceled")
            return "Canceled"
        return ""
    }
    function feeText(n) {
        if (Number(n) === 0)
            return "No fee"
        return grouped(n)
    }
    function isTxid(value) {
        return /^[0-9a-fA-F]{64}$/.test(String(value || ""))
    }
    function explorer() {
        if (!isTxid(entry.txid))
            return ""
        if (entry.source === "onchain" || entry.type === "Onchain")
            return "https://mempool.space/tx/" + entry.txid
        return ""
    }
    function chainTxid(value) {
        var text = String(value || "")
        var cut = text.lastIndexOf(":")
        if (cut > 0 && isTxid(text.substring(0, cut)))
            return text.substring(0, cut).toLowerCase()
        if (isTxid(text))
            return text.toLowerCase()
        return ""
    }
    function boostTxid() {
        if (entry.canceled || entry.confirmed)
            return ""
        var status = String(entry.status || "")
        if (status === "failed" || status === "canceled" || status === "successful")
            return ""
        var onchainWallet = entry.source === "onchain"
        var onchainMove = entry.kind === "send-onchain" || entry.kind === "offboard"
                || entry.kind === "exit" || entry.kind === "onboard"
        if (!onchainWallet && !onchainMove)
            return ""
        if (!onchainWallet && status !== "pending")
            return ""
        var anchor = chainTxid(entry.chain_anchor)
        if (anchor.length)
            return anchor
        if (isTxid(entry.txid))
            return String(entry.txid).toLowerCase()
        return ""
    }
    function copy(text) {
        var clean = addressOf(text)
        if (!clean.length)
            clean = String(text || "")
        if (!clean.length)
            return
        Clipboard.text = clean
        copiedFlash = true
        copyTimer.restart()
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader {
                title: "Payment details"
                description: page.statusLabel()
            }

            Item {
                anchors.horizontalCenter: parent.horizontalCenter
                width: Theme.iconSizeLarge
                height: width
                Rectangle {
                    anchors.fill: parent
                    radius: width / 2
                    color: "#f7931a"
                    opacity: 0.16
                }
                RailIcon {
                    anchors.centerIn: parent
                    width: parent.width * 0.5
                    height: width
                    rail: entry.rail || "onchain"
                    color: "#f7931a"
                }
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.secondaryColor
                font.pixelSize: Theme.fontSizeExtraSmall
                font.bold: true
                font.capitalization: Font.AllUppercase
                text: page.actionLabel()
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeHuge
                font.bold: true
                textFormat: Text.RichText
                text: page.grouped(page.magnitude())
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.secondaryColor
                visible: page.rate > 0 && Wallet.rateCurrency === Wallet.currency
                text: "≈ " + SendParse.formatMoney(page.magnitude(), page.rate, Wallet.currency)
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: page.place().length > 0
                color: Theme.secondaryColor
                font.pixelSize: Theme.fontSizeExtraSmall
                font.bold: true
                font.capitalization: Font.AllUppercase
                text: entry.direction === "outgoing" ? "Paid to" : "Received on"
            }
            BackgroundItem {
                width: parent.width
                height: visible ? Theme.itemSizeSmall : 0
                visible: page.place().length > 0
                onClicked: page.copy(page.place())
                Label {
                    anchors.centerIn: parent
                    width: parent.width - Theme.horizontalPageMargin * 2
                    horizontalAlignment: Text.AlignHCenter
                    elide: Text.ElideMiddle
                    maximumLineCount: 2
                    wrapMode: Text.Wrap
                    text: page.copiedFlash ? "Copied" : page.place()
                }
            }

            AcceleratorBoost {
                id: boost
                txid: page.boostTxid()
            }

            SectionHeader { text: "Overview" }
            DetailLine {
                label: String(entry.date_label || "").length ? "Confirmation" : "Date & time"
                value: page.whenText()
            }
            DetailLine { label: "Route"; value: (entry.canceled ? "Canceled " : "") + (entry.label || "") }
            DetailLine {
                label: page.entry.has_onchain_fee && page.entry.has_offchain_fee ? "Offchain fee" : "Fee"
                value: page.entry.has_offchain_fee ? page.feeText(page.entry.offchain_fee) : ""
            }
            DetailLine {
                label: page.entry.has_offchain_fee && page.entry.has_onchain_fee ? "Onchain fee" : "Fee"
                value: page.entry.has_onchain_fee ? page.feeText(page.entry.onchain_fee) : ""
            }

            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                onClicked: page.more = !page.more
                Label {
                    x: Theme.horizontalPageMargin
                    anchors.verticalCenter: parent.verticalCenter
                    text: "More details"
                }
                Label {
                    anchors.right: parent.right
                    anchors.rightMargin: Theme.horizontalPageMargin
                    anchors.verticalCenter: parent.verticalCenter
                    color: Theme.secondaryColor
                    text: page.more ? "Hide" : "Show"
                }
            }

            Column {
                width: parent.width
                visible: page.more
                spacing: Theme.paddingSmall
                DetailLine { label: "Payment ID"; value: entry.id || ""; copyable: true }
                DetailLine { label: "Transaction ID"; value: entry.txid || ""; copyable: true }
                DetailLine {
                    label: "Explorer"
                    value: page.explorer()
                    link: true
                }
                DetailLine {
                    label: "Chain status"
                    value: entry.source === "onchain" || entry.has_balance_change
                           ? (entry.confirmed ? "Confirmed" : "Unconfirmed") : ""
                }
                DetailLine {
                    label: "Balance change"
                    value: entry.has_balance_change ? page.grouped(entry.balance_change) : ""
                }
                DetailLine {
                    label: "Block height"
                    value: Number(entry.height) > 0 ? String(entry.height) : ""
                }
                DetailLine { label: "Block hash"; value: entry.block_hash || ""; copyable: true }
                DetailLine { label: "Movement type"; value: page.kindLabel() }
                DetailLine {
                    label: "Movement ID"
                    value: Number(entry.movement_id) > 0 ? String(entry.movement_id) : ""
                    copyable: true
                }
                DetailLine {
                    label: "Subsystem"
                    value: entry.subsystem
                           ? (entry.subsystem_kind ? entry.subsystem + " (" + entry.subsystem_kind + ")" : entry.subsystem)
                           : ""
                }
                DetailLine {
                    label: "Chain anchor"
                    value: entry.chain_anchor && entry.chain_anchor !== entry.txid ? entry.chain_anchor : ""
                    copyable: true
                }
                DetailLine {
                    label: "Intended change"
                    value: Number(entry.movement_id) > 0 ? page.grouped(entry.intended) : ""
                }
                DetailLine {
                    label: "Effective change"
                    value: Number(entry.movement_id) > 0 ? page.grouped(entry.effective) : ""
                }
                DetailLine { label: "Status"; value: page.movementStatus() }

                Repeater {
                    model: sentModel
                    delegate: Column {
                        width: parent.width
                        DetailLine {
                            label: index === 0 ? "Sent to" : ""
                            value: address
                            copyable: true
                        }
                        DetailLine {
                            label: "Amount"
                            value: Number(amount) > 0 ? page.grouped(amount) : ""
                        }
                    }
                }
                Repeater {
                    model: gotModel
                    delegate: Column {
                        width: parent.width
                        DetailLine {
                            label: index === 0 ? "Received on" : ""
                            value: address
                            copyable: true
                        }
                        DetailLine {
                            label: "Amount"
                            value: Number(amount) > 0 ? page.grouped(amount) : ""
                        }
                    }
                }
            }

        }
        VerticalScrollDecorator {}
    }
}
