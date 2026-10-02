import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Dialog {
    id: dialog
    property var ids: []
    property double amountSat: 0
    property int vtxoCount: 1

    canAccept: Wallet.hasRefreshFee

    function money(sats) {
        var text = SendParse.group(sats) + " sats"
        if (Wallet.btcUsd > 0 && Wallet.rateCurrency === Wallet.currency) {
            var fiat = SendParse.formatMoney(sats, Wallet.btcUsd, Wallet.currency)
            if (fiat.length)
                text += " (" + fiat + ")"
        }
        return text
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            DialogHeader { acceptText: "Refresh" }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeExtraLarge
                font.bold: true
                textFormat: Text.RichText
                text: dialog.money(dialog.amountSat)
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                text: dialog.vtxoCount === 1
                      ? "This refreshes this VTXO in a delegated Ark round."
                      : "This refreshes the selected VTXOs in a delegated Ark round."
            }
            DetailLine {
                label: "VTXOs selected"
                value: String(dialog.vtxoCount)
            }
            DetailLine {
                label: "Refresh fee"
                value: Wallet.hasRefreshFee ? dialog.money(Wallet.refreshFee) : ""
            }
            DetailLine {
                label: "Amount after fee"
                value: Wallet.hasRefreshFee ? dialog.money(Wallet.refreshNet) : ""
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                color: Theme.secondaryColor
                visible: !Wallet.hasRefreshFee && Wallet.busy
                text: "Estimating fee…"
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: Wallet.error.length > 0 && Wallet.lastOp === "refresh_fee"
                text: Wallet.error
            }
        }
        VerticalScrollDecorator {}
    }
}
