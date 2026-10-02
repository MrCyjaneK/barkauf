import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

BackgroundItem {
    id: boost

    property string txid: ""
    property var quote: {
        var raw = Wallet.acceleratorJson
        var parsed = {}
        try {
            parsed = JSON.parse(raw || "{}")
        } catch (e) {
            parsed = {}
        }
        if (String(parsed.txid || "") !== boost.txid)
            return {}
        return parsed
    }

    width: parent ? parent.width : 0
    visible: txid.length > 0
    height: visible ? Math.max(Theme.itemSizeLarge, body.height + Theme.paddingLarge * 2) : 0
    onClicked: pageStack.animatorPush(Qt.resolvedUrl("AcceleratorPage.qml"), { txid: boost.txid })

    function reload() {
        if (txid.length)
            Wallet.accelPreview(txid)
    }
    function sats(n) {
        return SendParse.group(n) + " sats"
    }
    function cheapest() {
        var list = quote.options || []
        var best = 0
        for (var i = 0; i < list.length; ++i) {
            var n = Number(list[i]) || 0
            if (n > 0 && (best === 0 || n < best))
                best = n
        }
        if (best === 0)
            best = Number(quote.cost || 0)
        return best
    }
    function estimated(fee) {
        return Number(quote.mempool_base_fee || 0) + Number(quote.vsize_fee || 0) + Number(fee || 0)
    }
    function ready() {
        return !!(quote.has_estimate || quote.has_boost || quote.error || quote.estimate_error)
    }
    function problem() {
        return String(quote.error || quote.estimate_error || "")
    }
    function summary() {
        var err = boost.problem()
        if (err.length)
            return err
        if (quote.has_boost)
            return "Accelerating"
        if (!boost.ready())
            return "Asking mempool for a quote…"
        if (quote.unavailable)
            return "Not available right now"
        var price = boost.cheapest()
        if (quote.has_estimate && price > 0)
            return "From " + boost.sats(boost.estimated(price))
        return "Open to choose a bid"
    }

    Item {
        id: body
        x: Theme.horizontalPageMargin
        width: parent.width - 2 * Theme.horizontalPageMargin - Theme.iconSizeSmall - Theme.paddingMedium
        height: Math.max(mark.height, textCol.height)
        anchors.verticalCenter: parent.verticalCenter

        MempoolMark {
            id: mark
            width: Theme.iconSizeMedium
            height: width
            anchors.verticalCenter: parent.verticalCenter
        }
        Column {
            id: textCol
            anchors.left: mark.right
            anchors.leftMargin: Theme.paddingLarge
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            spacing: Theme.paddingSmall

            Label {
                width: parent.width
                text: "mempool"
                font.bold: true
                truncationMode: TruncationMode.Fade
            }
            Label {
                width: parent.width
                color: Theme.secondaryColor
                font.pixelSize: Theme.fontSizeSmall
                text: "Public accelerator"
                truncationMode: TruncationMode.Fade
            }
            Item {
                width: parent.width
                height: Math.max(spinner.running ? spinner.height : 0, statusLabel.implicitHeight)

                BusyIndicator {
                    id: spinner
                    size: BusyIndicatorSize.Small
                    running: !boost.ready() && boost.problem().length === 0
                    visible: running
                    anchors.verticalCenter: parent.verticalCenter
                }
                Label {
                    id: statusLabel
                    anchors.left: parent.left
                    anchors.leftMargin: spinner.visible ? spinner.width + Theme.paddingSmall : 0
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    wrapMode: Text.Wrap
                    maximumLineCount: 3
                    elide: Text.ElideRight
                    color: boost.problem().length ? Theme.highlightColor : Theme.secondaryColor
                    font.pixelSize: Theme.fontSizeSmall
                    text: boost.summary()
                }
            }
        }
    }

    Image {
        anchors.right: parent.right
        anchors.rightMargin: Theme.horizontalPageMargin
        anchors.verticalCenter: parent.verticalCenter
        source: "image://theme/icon-m-right"
    }
}
