import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    objectName: "balance"
    property double rate: Wallet.btcUsd
    property bool syncQueued: false
    property bool detailsOpen: false

    function group(n) {
        return SendParse.group(n)
    }
    function pendingSats() {
        return Number(Wallet.onchainPending)
                + Number(Wallet.pendingSend)
                + Number(Wallet.pendingInRound)
                + Number(Wallet.pendingExit)
                + Number(Wallet.pendingBoard)
                + Number(Wallet.claimableReceive)
    }
    function blocksUntilRefresh() {
        var tip = Number(Wallet.tipHeight)
        var at = Number(Wallet.refreshHeight)
        if (!(tip > 0) || !(at > 0))
            return -1
        return at - tip
    }
    function refreshSummary() {
        if (Number(Wallet.refreshCount) <= 0)
            return "None due"
        var text = page.group(Wallet.refreshSats) + " sats"
        var left = blocksUntilRefresh()
        if (left > 1)
            text += " · " + left + " blocks left"
        else if (left === 1)
            text += " · 1 block left"
        else if (left <= 0 && Number(Wallet.refreshHeight) > 0 && Number(Wallet.tipHeight) > 0)
            text += " · due now"
        if (Number(Wallet.refreshHeight) > 0)
            text += " · block " + Wallet.refreshHeight
        return text
    }

    function syncIfIdle() {
        if (Wallet.busy) {
            syncQueued = true
            return
        }
        syncQueued = false
        Wallet.sync()
    }

    function loadRate() {
        if (Wallet.rateCurrency === Wallet.currency && Wallet.btcUsd > 0)
            rate = Wallet.btcUsd
        else
            rate = 0
        SendParse.loadBtcFiat(Wallet.currency, function (next) {
            if (next > 0) {
                rate = next
                Wallet.rememberRate(next)
            }
        })
    }

    onStatusChanged: {
        if (status !== PageStatus.Active)
            return
        pageStack.pushAttached(Qt.resolvedUrl("SendPage.qml"))
        loadRate()
        if (Wallet.hasWallet)
            Wallet.loadRefreshDue()
        syncIfIdle()
    }

    Connections {
        target: Wallet
        onBusyChanged: {
            if (!Wallet.busy && page.syncQueued && page.status === PageStatus.Active)
                page.syncIfIdle()
        }
    }

    SilicaListView {
        id: history
        anchors.fill: parent
        model: Wallet.history

        PullDownMenu {
            MenuItem {
                text: "Settings"
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("SettingsPage.qml"))
            }
        }

        header: Column {
            width: history.width
            spacing: Theme.paddingSmall

            PageHeader {
                title: "Barkauf"
                description: Wallet.busy && Wallet.activity.length > 0 ? Wallet.activity : ""
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: page.rate > 0 && Wallet.rateCurrency === Wallet.currency
                color: Theme.secondaryColor
                font.pixelSize: Theme.fontSizeLarge
                text: SendParse.formatMoney(Wallet.totalSats, page.rate, Wallet.currency)
            }

            Item {
                anchors.horizontalCenter: parent.horizontalCenter
                width: balanceRow.width
                height: balanceRow.height

                Row {
                    id: balanceRow
                    height: balanceLabel.implicitHeight
                    spacing: Theme.paddingSmall
                    Label {
                        id: balanceLabel
                        anchors.verticalCenter: parent.verticalCenter
                        font.pixelSize: String(Wallet.totalSats).length > 8
                                         ? Theme.fontSizeExtraLarge : Theme.fontSizeHuge
                        font.bold: true
                        textFormat: Text.RichText
                        text: page.group(Wallet.totalSats) + " sats"
                    }
                    Image {
                        anchors.verticalCenter: parent.verticalCenter
                        width: Theme.iconSizeSmall
                        height: width
                        source: "image://theme/icon-m-down"
                        rotation: page.detailsOpen ? 180 : 0
                    }
                }
                MouseArea {
                    anchors.fill: parent
                    onClicked: page.detailsOpen = !page.detailsOpen
                }
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: page.pendingSats() > 0
                color: Theme.highlightColor
                font.pixelSize: Theme.fontSizeSmall
                textFormat: Text.RichText
                text: "Pending " + page.group(page.pendingSats()) + " sats"
            }
            BackgroundItem {
                width: parent.width
                visible: Wallet.refreshCount > 0
                height: visible ? refreshNote.height + Theme.paddingLarge : 0
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("VtxosPage.qml"), { filter: "due" })

                Column {
                    id: refreshNote
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.paddingSmall / 2

                    Label {
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: "#f97316"
                        font.bold: true
                        text: Wallet.refreshCount === 1
                              ? "1 VTXO needs a refresh"
                              : Wallet.refreshCount + " VTXOs need a refresh"
                    }
                }
            }
            BackgroundItem {
                width: parent.width
                visible: Wallet.roundSummary.length > 0
                height: visible ? refreshingNote.height + Theme.paddingLarge : 0
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("VtxosPage.qml"), { filter: "refreshing" })

                Column {
                    id: refreshingNote
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.paddingSmall / 2

                    Label {
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: Theme.highlightColor
                        font.bold: true
                        text: Wallet.roundSummary
                    }
                    Label {
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: Theme.secondaryColor
                        font.pixelSize: Theme.fontSizeSmall
                        visible: Wallet.refreshInRound > 0
                        text: Wallet.refreshInRound === 1
                              ? "1 VTXO in this round"
                              : Wallet.refreshInRound + " VTXOs in this round"
                    }
                }
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.secondaryColor
                font.pixelSize: Theme.fontSizeExtraSmall
                text: "Swipe for receive or send"
            }

            Column {
                width: parent.width
                visible: page.detailsOpen
                spacing: Theme.paddingSmall

                SectionHeader { text: "Onchain" }
                Repeater {
                    model: [
                        { label: "Total", value: Wallet.onchainTotal, show: true },
                        { label: "Confirmed", value: Wallet.onchainConfirmed, show: true },
                        { label: "Pending", value: Wallet.onchainPending, show: true }
                    ]
                    delegate: Item {
                        x: Theme.horizontalPageMargin
                        width: parent.width - 2 * x
                        height: modelData.show ? (line.implicitHeight + Theme.paddingSmall) : 0
                        visible: modelData.show
                        Label { id: line; text: modelData.label; color: Theme.secondaryColor }
                        Label {
                            anchors.right: parent.right
                            textFormat: Text.RichText
                            text: page.group(modelData.value) + " sats"
                        }
                    }
                }

                SectionHeader { text: "Offchain" }
                Repeater {
                    model: [
                        { label: "Total", value: Wallet.offchainTotal, show: true },
                        { label: "Spendable", value: Wallet.spendableSats, show: true },
                        { label: "Pending send", value: Wallet.pendingSend, show: true },
                        { label: "Pending in round", value: Wallet.pendingInRound, show: true },
                        { label: "Pending exit", value: Wallet.pendingExit, show: true },
                        { label: "Pending board", value: Wallet.pendingBoard, show: true },
                        { label: "Claimable", value: Wallet.claimableReceive, show: true }
                    ]
                    delegate: Item {
                        x: Theme.horizontalPageMargin
                        width: parent.width - 2 * x
                        height: modelData.show ? (line.implicitHeight + Theme.paddingSmall) : 0
                        visible: modelData.show
                        Label { id: line; text: modelData.label; color: Theme.secondaryColor }
                        Label {
                            anchors.right: parent.right
                            textFormat: Text.RichText
                            text: page.group(modelData.value) + " sats"
                        }
                    }
                }

                SectionHeader { text: "VTXO refresh" }
                Repeater {
                    model: [
                        { label: "VTXOs due", value: String(Wallet.refreshCount) },
                        { label: "In round", value: String(Wallet.refreshInRound) },
                        { label: "Round", value: Wallet.roundSummary.length ? Wallet.roundSummary : "None" },
                        { label: "Amount", value: page.group(Wallet.refreshSats) + " sats" },
                        { label: "Next block", value: Number(Wallet.refreshHeight) > 0 ? String(Wallet.refreshHeight) : "None" },
                        { label: "Blocks left", value: page.blocksUntilRefresh() < 0
                                ? "Unknown" : String(page.blocksUntilRefresh()) },
                        { label: "Chain tip", value: Number(Wallet.tipHeight) > 0 ? String(Wallet.tipHeight) : "Unknown" }
                    ]
                    delegate: Item {
                        x: Theme.horizontalPageMargin
                        width: parent.width - 2 * x
                        height: refreshLine.implicitHeight + Theme.paddingSmall
                        Label {
                            id: refreshLine
                            text: modelData.label
                            color: Theme.secondaryColor
                        }
                        Label {
                            anchors.right: parent.right
                            textFormat: Text.RichText
                            text: modelData.value
                        }
                    }
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: Wallet.error.length > 0
                text: Wallet.error
            }
            SectionHeader { text: "Activity" }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                visible: !Wallet.busy && Wallet.history.length === 0
                font.bold: true
                text: "No activity yet"
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                visible: !Wallet.busy && Wallet.history.length === 0
                text: "Receive or send bitcoin to start your history."
            }
        }

        delegate: ListItem {
            id: row
            contentHeight: Theme.itemSizeSmall
            onClicked: pageStack.animatorPush(Qt.resolvedUrl("TxPage.qml"), {
                entry: row.payload(),
                rate: page.rate
            })

            property var entry: model.label !== undefined ? model : modelData
            property string rail: entry.rail || "onchain"
            property string txType: entry.label || "Onchain"
            property bool canceled: !!entry.canceled || entry.status === "canceled" || entry.status === "failed"
            property bool transfer: !!entry.transfer || entry.kind === "onboard"
            property string direction: entry.direction || (Number(entry.amount) < 0 ? "outgoing" : "incoming")
            property int magnitude: Math.abs(Number(entry.amount || 0))
            property color tone: {
                if (row.canceled)
                    return Theme.secondaryColor
                if (row.transfer)
                    return "#f97316"
                if (row.direction === "outgoing")
                    return "#ff5c5c"
                return "#3dcc8a"
            }

            function payload() {
                var e = row.entry
                return {
                    id: String(e.id || ""),
                    label: row.txType,
                    rail: row.rail,
                    type: String(e.type || ""),
                    kind: String(e.kind || ""),
                    amount: row.magnitude,
                    direction: row.direction,
                    transfer: row.transfer,
                    canceled: row.canceled,
                    created_at: String(e.created_at || ""),
                    date_label: String(e.date_label || ""),
                    destination: String(e.destination || ""),
                    txid: String(e.txid || ""),
                    status: String(e.status || ""),
                    has_offchain_fee: !!e.has_offchain_fee,
                    offchain_fee: Number(e.offchain_fee || 0),
                    has_onchain_fee: !!e.has_onchain_fee,
                    onchain_fee: Number(e.onchain_fee || 0),
                    subsystem: String(e.subsystem || ""),
                    subsystem_kind: String(e.subsystem_kind || ""),
                    height: Number(e.height || 0),
                    block_hash: String(e.block_hash || ""),
                    source: String(e.source || ""),
                    intended: Number(e.intended || 0),
                    effective: Number(e.effective || 0),
                    movement_id: Number(e.movement_id || 0),
                    confirmed: !!e.confirmed,
                    chain_anchor: String(e.chain_anchor || ""),
                    has_balance_change: !!e.has_balance_change,
                    balance_change: Number(e.balance_change || 0),
                    sent_to: e.sent_to || [],
                    received_on: e.received_on || []
                }
            }
            function whenText() {
                var label = String(row.entry.date_label || "")
                if (label.indexOf("Confirmed at block") === 0)
                    label = ""
                if (label.length)
                    return label
                var when = String(row.entry.created_at || "")
                if (!when.length)
                    return ""
                var d = new Date(when)
                if (isNaN(d.getTime()))
                    return when
                return Qt.formatDateTime(d, "d MMM")
            }
            function statusText() {
                var s = String(row.entry.status || "")
                if (s === "pending")
                    return "Pending"
                if (s === "failed")
                    return "Failed"
                if (s === "canceled")
                    return "Canceled"
                if (s === "successful")
                    return "Successful"
                return ""
            }
            function satsText() {
                var grouped = SendParse.group(row.magnitude)
                if (row.canceled || row.transfer)
                    return grouped + " sats"
                return (row.direction === "outgoing" ? "−" : "+") + grouped + " sats"
            }

            Row {
                id: body
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                height: parent.height
                spacing: Theme.paddingMedium

                Item {
                    width: Theme.iconSizeSmall
                    height: width
                    anchors.verticalCenter: parent.verticalCenter
                    Rectangle {
                        anchors.fill: parent
                        radius: width / 2
                        color: row.tone
                        opacity: 0.16
                    }
                    RailIcon {
                        anchors.centerIn: parent
                        width: parent.width * 0.62
                        height: width
                        rail: row.rail
                        color: row.tone
                    }
                }

                Column {
                    width: parent.width - Theme.iconSizeSmall - amounts.implicitWidth - 2 * parent.spacing
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 0
                    Label {
                        width: parent.width
                        truncationMode: TruncationMode.Fade
                        color: row.canceled ? Theme.secondaryColor : Theme.primaryColor
                        text: (row.canceled ? "Canceled " : "") + row.txType
                    }
                    Label {
                        width: parent.width
                        truncationMode: TruncationMode.Fade
                        color: Theme.secondaryColor
                        font.pixelSize: Theme.fontSizeExtraSmall
                        text: {
                            var when = row.whenText()
                            var status = row.statusText()
                            if (status.length && status !== "Successful")
                                return when.length ? when + " · " + status : status
                            return when
                        }
                    }
                }

                Label {
                    id: amounts
                    anchors.verticalCenter: parent.verticalCenter
                    font.bold: true
                    color: row.tone
                    textFormat: Text.RichText
                    text: row.satsText()
                }
            }
        }
        VerticalScrollDecorator {}
    }
}
