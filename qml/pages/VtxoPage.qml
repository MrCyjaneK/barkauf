import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property var vtxo: ({})
    property bool awaitingFee: false
    property bool awaitingRefresh: false
    property string notice: ""
    property bool noticeFailed: false

    function statusText() {
        if (vtxo.inRound)
            return vtxo.roundLabel || "Scheduled in a round"
        if (vtxo.expired)
            return "Expired"
        if (vtxo.needsRefresh || vtxo.expiring)
            return "Expiring"
        return vtxo.state || ""
    }

    function needsAttention() {
        return !vtxo.inRound && vtxo.state !== "Locked" && (vtxo.expired || vtxo.expiring || vtxo.needsRefresh)
    }

    function canRefresh() {
        return !vtxo.inRound && vtxo.state !== "Locked" && String(vtxo.id || "").length > 0
    }

    function askRefresh() {
        if (!canRefresh() || Wallet.busy)
            return
        notice = ""
        noticeFailed = false
        awaitingFee = true
        Wallet.estimateRefresh(JSON.stringify([vtxo.id]))
    }

    function openRefreshDialog() {
        var dialog = pageStack.push(Qt.resolvedUrl("RefreshDialog.qml"), {
            ids: [vtxo.id],
            amountSat: Number(vtxo.amount || 0),
            vtxoCount: 1
        })
        dialog.accepted.connect(function () {
            page.awaitingRefresh = true
            page.notice = ""
            page.noticeFailed = false
            Wallet.refreshVtxos(JSON.stringify([page.vtxo.id]))
        })
    }

    function finishRefresh() {
        awaitingRefresh = false
        if (Wallet.refreshScheduled) {
            noticeFailed = Wallet.error.length > 0
            notice = Wallet.refreshDetail.length
                    ? Wallet.refreshDetail
                    : "Refresh scheduled. This VTXO will renew in a delegated Ark round."
            if (Wallet.error.length)
                notice = notice + " " + Wallet.error
        } else {
            noticeFailed = true
            notice = Wallet.error.length
                    ? Wallet.error
                    : (Wallet.refreshDetail.length
                       ? Wallet.refreshDetail
                       : "Bark did not schedule a refresh for this VTXO.")
        }
        Wallet.loadVtxos()
    }

    Connections {
        target: Wallet
        onChanged: {
            if (page.awaitingFee && Wallet.lastOp === "refresh_fee") {
                page.awaitingFee = false
                if (!Wallet.hasRefreshFee) {
                    page.noticeFailed = true
                    page.notice = Wallet.error.length ? Wallet.error : "Could not estimate the refresh fee"
                    return
                }
                page.openRefreshDialog()
                return
            }
            if (page.awaitingRefresh && Wallet.lastOp === "refresh_vtxos")
                page.finishRefresh()
        }
    }

    SilicaFlickable {
        anchors.fill: parent
        anchors.bottomMargin: page.canRefresh() ? actions.height : 0
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader {
                title: "VTXO"
                description: page.statusText()
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                visible: !!vtxo.inRound
                color: Theme.highlightColor
                font.bold: true
                text: vtxo.roundLabel || "Scheduled in a round"
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                visible: !!vtxo.inRound
                color: Theme.secondaryColor
                text: "Bark will not submit this VTXO again until the round finishes."
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                visible: page.needsAttention()
                color: vtxo.expired ? "#ef4444" : "#f97316"
                font.bold: true
                text: vtxo.expired ? "VTXO expired" : "VTXO expiring soon"
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                visible: page.needsAttention()
                color: Theme.secondaryColor
                text: "Refresh this VTXO to keep it available."
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeExtraLarge
                font.bold: true
                textFormat: Text.RichText
                text: SendParse.group(vtxo.amount || 0) + " sats"
            }

            SectionHeader { text: "Details" }
            DetailLine { label: "State"; value: vtxo.state || "" }
            DetailLine { label: "Kind"; value: vtxo.kind || "" }
            DetailLine {
                label: "Expires at block"
                value: Number(vtxo.expiry) > 0 ? String(vtxo.expiry) : ""
            }
            DetailLine {
                label: "Exit depth"
                value: Number(vtxo.exitDepth) > 0 ? String(vtxo.exitDepth) : ""
            }
            DetailLine { label: "VTXO ID"; value: vtxo.id || ""; copyable: true }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: page.noticeFailed ? "#ef4444" : Theme.highlightColor
                visible: page.notice.length > 0
                text: page.notice
            }
        }
        VerticalScrollDecorator {}
    }

    ButtonLayout {
        id: actions
        anchors.bottom: parent.bottom
        visible: page.canRefresh()
        Button {
            text: page.awaitingFee ? "Estimating…" : (page.awaitingRefresh ? "Refreshing…" : "Refresh VTXO")
            enabled: !page.awaitingFee && !page.awaitingRefresh
            onClicked: page.askRefresh()
        }
    }
}
