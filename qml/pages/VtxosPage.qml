import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property string filter: "all"
    property bool selecting: false
    property var picked: ({})
    property bool awaitingFee: false
    property bool awaitingRefresh: false
    property string notice: ""
    property bool noticeFailed: false
    property var pendingIds: []
    property string pendingJson: "[]"
    property double pendingAmount: 0
    property int pendingCount: 0
    property bool loading: false
    property int builtSerial: -1

    ListModel { id: rows }

    function mark(item) {
        var tip = Number(Wallet.tipHeight)
        var delta = Number(Wallet.vtxoExitDelta)
        var expiry = Number(item.expiry_height || 0)
        var expired = tip > 0 && item.state !== "Locked" && expiry > 0 && expiry <= tip
        var expiring = !expired && item.state === "Spendable" && tip > 0 && delta > 0
                && expiry > tip && (expiry - tip) <= delta
        return { expired: expired, expiring: expiring }
    }

    function rebuild() {
        rows.clear()
        var list = Wallet.vtxos || []
        for (var i = 0; i < list.length; ++i) {
            var item = list[i]
            var flags = mark(item)
            var due = !!item.needs_refresh && !item.in_round
            var refreshing = !!item.in_round
            var keep = filter === "all"
                    || (filter === "active" && item.state === "Spendable" && !flags.expiring && !flags.expired && !refreshing)
                    || (filter === "expiring" && flags.expiring && !refreshing)
                    || (filter === "expired" && flags.expired)
                    || (filter === "locked" && item.state === "Locked" && !refreshing)
                    || (filter === "due" && due)
                    || (filter === "refreshing" && refreshing)
            if (!keep)
                continue
            rows.append({
                vid: String(item.id || ""),
                amount: Number(item.amount || 0),
                expiry: Number(item.expiry_height || 0),
                kind: String(item.kind || ""),
                vtxoState: String(item.state || ""),
                exitDepth: Number(item.exit_depth || 0),
                expired: flags.expired,
                expiring: flags.expiring,
                needsRefresh: due,
                inRound: refreshing,
                roundLabel: String(item.round_label || "")
            })
        }
    }

    function tone(item) {
        if (item.inRound)
            return Theme.highlightColor
        if (item.state === "Locked")
            return Theme.secondaryColor
        if (item.expired)
            return "#ef4444"
        if (item.expiring || item.needsRefresh)
            return "#f97316"
        if (item.state === "Spendable")
            return "#22c55e"
        return Theme.secondaryColor
    }

    function emptyText() {
        if (filter === "active")
            return "No active VTXOs found"
        if (filter === "expiring")
            return "No expiring VTXOs found"
        if (filter === "expired")
            return "No expired VTXOs found"
        if (filter === "locked")
            return "No locked VTXOs found"
        if (filter === "due")
            return "No VTXOs need a refresh"
        if (filter === "refreshing")
            return "No VTXOs are scheduled in a round"
        return "No VTXOs found"
    }

    function loadFraction() {
        var found = String(Wallet.activity).match(/(\d+) of (\d+)/)
        if (!found)
            return -1
        var total = Number(found[2])
        if (!(total > 0))
            return -1
        return Number(found[1]) / total
    }

    function headerText() {
        if (loading)
            return Wallet.activity.length > 0 ? Wallet.activity : "Loading VTXOs"
        if (selecting)
            return selectedCount() + " selected"
        if (filter === "due")
            return "Refresh these before they expire"
        if (filter === "refreshing")
            return Wallet.roundSummary.length ? Wallet.roundSummary : "In a round"
        return filter.charAt(0).toUpperCase() + filter.slice(1)
    }

    function isPicked(id) {
        return !!picked[id]
    }

    function selectedCount() {
        var n = 0
        var map = picked
        for (var k in map) {
            if (map[k])
                n++
        }
        return n
    }

    function selectedIds() {
        var ids = []
        var map = picked
        for (var k in map) {
            if (map[k])
                ids.push(k)
        }
        return ids
    }

    function selectedAmount() {
        var total = 0
        var map = picked
        var list = Wallet.vtxos || []
        for (var i = 0; i < list.length; ++i) {
            if (map[String(list[i].id || "")])
                total += Number(list[i].amount || 0)
        }
        return total
    }

    function toggle(id) {
        var copy = {}
        for (var k in picked)
            copy[k] = picked[k]
        if (copy[id])
            delete copy[id]
        else
            copy[id] = true
        picked = copy
    }

    function stopSelecting() {
        selecting = false
        picked = ({})
    }

    function selectDue() {
        var copy = {}
        var list = Wallet.vtxos || []
        for (var i = 0; i < list.length; ++i) {
            var item = list[i]
            var flags = mark(item)
            if (item.state === "Locked" || item.in_round || flags.expired)
                continue
            if (item.needs_refresh || flags.expiring)
                copy[String(item.id || "")] = true
        }
        picked = copy
    }

    function selectVisible() {
        var copy = {}
        for (var i = 0; i < rows.count; ++i) {
            var row = rows.get(i)
            if (row.vtxoState !== "Locked" && !row.inRound && !row.expired)
                copy[row.vid] = true
        }
        picked = copy
    }

    function askRefresh(ids, amount, count) {
        if (!ids || !ids.length) {
            noticeFailed = true
            notice = "Select at least one VTXO."
            return
        }
        notice = ""
        noticeFailed = false
        pendingIds = ids
        pendingJson = JSON.stringify(ids)
        pendingAmount = amount
        pendingCount = count
        awaitingFee = true
        Wallet.estimateRefresh(pendingJson)
    }

    function openRefreshDialog() {
        var dialog = pageStack.push(Qt.resolvedUrl("RefreshDialog.qml"), {
            ids: pendingIds,
            amountSat: pendingAmount,
            vtxoCount: pendingCount
        })
        dialog.accepted.connect(function () {
            page.awaitingRefresh = true
            page.notice = ""
            page.noticeFailed = false
            Wallet.refreshVtxos(page.pendingJson)
        })
    }

    function finishRefresh() {
        awaitingRefresh = false
        if (Wallet.refreshScheduled) {
            stopSelecting()
            noticeFailed = Wallet.error.length > 0
            notice = Wallet.refreshDetail.length
                    ? Wallet.refreshDetail
                    : "Refresh scheduled. The selected VTXOs will renew in a delegated Ark round."
            if (Wallet.error.length)
                notice = notice + " " + Wallet.error
        } else {
            noticeFailed = true
            notice = Wallet.error.length
                    ? Wallet.error
                    : (Wallet.refreshDetail.length
                       ? Wallet.refreshDetail
                       : "Bark did not schedule a refresh for the selected VTXOs.")
        }
        page.reloadVtxos()
    }

    function reloadVtxos() {
        loading = true
        Wallet.loadVtxos()
    }

    onFilterChanged: rebuild()
    Component.onCompleted: reloadVtxos()

    Connections {
        target: Wallet
        onBusyChanged: {
            if (!Wallet.busy)
                page.loading = false
        }
        onChanged: {
            if (Wallet.busy && Wallet.activity === "Loading VTXOs")
                page.loading = true
            if (Wallet.vtxoSerial !== page.builtSerial) {
                page.builtSerial = Wallet.vtxoSerial
                page.rebuild()
            }
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

    SilicaListView {
        anchors.fill: parent
        anchors.bottomMargin: page.selecting ? refreshBar.height : 0
        model: rows

        PullDownMenu {
            MenuItem {
                text: page.selecting ? "Cancel" : "Select"
                onClicked: {
                    if (page.selecting)
                        page.stopSelecting()
                    else
                        page.selecting = true
                }
            }
            MenuItem {
                text: "Select due"
                visible: page.selecting
                onClicked: page.selectDue()
            }
            MenuItem {
                text: "Select visible"
                visible: page.selecting
                onClicked: page.selectVisible()
            }
            MenuItem {
                text: "Clear"
                visible: page.selecting && page.selectedCount() > 0
                onClicked: page.picked = ({})
            }
            MenuItem { text: "Locked"; onClicked: page.filter = "locked" }
            MenuItem { text: "Expired"; onClicked: page.filter = "expired" }
            MenuItem { text: "Expiring"; onClicked: page.filter = "expiring" }
            MenuItem { text: "Refreshing"; onClicked: page.filter = "refreshing" }
            MenuItem { text: "Due"; onClicked: page.filter = "due" }
            MenuItem { text: "Active"; onClicked: page.filter = "active" }
            MenuItem { text: "All"; onClicked: page.filter = "all" }
        }

        header: Column {
            width: parent.width
            PageHeader {
                title: page.selecting ? "Select VTXOs" : "VTXOs"
                description: page.headerText()
            }
            ProgressBar {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                visible: page.loading
                indeterminate: page.loadFraction() < 0
                minimumValue: 0
                maximumValue: 1
                value: page.loadFraction() < 0 ? 0 : page.loadFraction()
            }
            BusyIndicator {
                anchors.horizontalCenter: parent.horizontalCenter
                size: BusyIndicatorSize.Large
                running: page.loading && rows.count === 0
                visible: running
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: page.noticeFailed ? "#ef4444" : Theme.highlightColor
                visible: page.notice.length > 0
                text: page.notice
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                visible: !page.loading && rows.count === 0
                text: page.emptyText()
            }
        }

        delegate: ListItem {
            id: row
            contentHeight: Theme.itemSizeSmall
            opacity: page.selecting && (vtxoState === "Locked" || inRound || expired) ? 0.45 : 1
            onClicked: {
                if (page.selecting) {
                    if (vtxoState !== "Locked" && !inRound && !expired && !Wallet.busy)
                        page.toggle(vid)
                    return
                }
                pageStack.animatorPush(Qt.resolvedUrl("VtxoPage.qml"), {
                    vtxo: {
                        id: vid,
                        amount: amount,
                        expiry: expiry,
                        kind: kind,
                        state: vtxoState,
                        exitDepth: exitDepth,
                        expired: expired,
                        expiring: expiring,
                        needsRefresh: needsRefresh,
                        inRound: inRound,
                        roundLabel: roundLabel
                    }
                })
            }

            Label {
                id: markIcon
                visible: page.selecting
                x: Theme.horizontalPageMargin
                anchors.verticalCenter: parent.verticalCenter
                color: page.isPicked(vid) ? Theme.highlightColor : Theme.secondaryColor
                text: page.isPicked(vid) ? "✓" : "○"
            }
            Label {
                id: amountLabel
                x: page.selecting ? markIcon.x + markIcon.implicitWidth + Theme.paddingSmall : Theme.horizontalPageMargin
                anchors.verticalCenter: parent.verticalCenter
                width: Math.min(implicitWidth, parent.width * 0.48)
                color: page.tone({
                    state: vtxoState,
                    expired: expired,
                    expiring: expiring,
                    needsRefresh: needsRefresh,
                    inRound: inRound
                })
                font.bold: true
                textFormat: Text.RichText
                text: SendParse.group(amount) + " sats"
            }
            Label {
                anchors.right: parent.right
                anchors.rightMargin: Theme.horizontalPageMargin
                anchors.verticalCenter: parent.verticalCenter
                width: Math.min(implicitWidth, Math.max(0, parent.width - amountLabel.x - amountLabel.width - Theme.paddingMedium - Theme.horizontalPageMargin))
                color: Theme.secondaryColor
                font.pixelSize: Theme.fontSizeSmall
                truncationMode: TruncationMode.Fade
                text: inRound
                      ? (roundLabel.length ? roundLabel : "In a round")
                      : ((needsRefresh ? "Needs refresh · " + vtxoState : vtxoState) + " · block " + expiry)
            }
        }
        VerticalScrollDecorator {}
    }

    Item {
        id: refreshBar
        anchors.bottom: parent.bottom
        width: parent.width
        height: page.selecting ? refreshButton.height + Theme.paddingLarge : 0
        visible: page.selecting

        Button {
            id: refreshButton
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            anchors.bottomMargin: Theme.paddingLarge
            preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
            enabled: page.selectedCount() > 0 && !page.awaitingFee && !page.awaitingRefresh
            text: page.awaitingFee ? "Estimating…" : (page.awaitingRefresh ? "Refreshing…" : "Refresh")
            onClicked: page.askRefresh(page.selectedIds(), page.selectedAmount(), page.selectedCount())
        }
    }
}
