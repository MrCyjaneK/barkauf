import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

CoverBackground {
    Label {
        id: amount
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.verticalCenter: parent.verticalCenter
        anchors.verticalCenterOffset: Wallet.hasWallet
                                       && Wallet.btcUsd > 0
                                       && Wallet.rateCurrency === Wallet.currency
                                       ? -Theme.paddingLarge : 0
        width: parent.width - 2 * Theme.paddingLarge
        horizontalAlignment: Text.AlignHCenter
        wrapMode: Text.Wrap
        font.pixelSize: Theme.fontSizeLarge
        font.bold: true
        textFormat: Text.RichText
        text: Wallet.hasWallet ? (SendParse.group(Wallet.totalSats) + " sats") : "Barkauf"
    }

    Label {
        anchors.top: amount.bottom
        anchors.horizontalCenter: parent.horizontalCenter
        visible: Wallet.hasWallet && Wallet.btcUsd > 0 && Wallet.rateCurrency === Wallet.currency
        color: Theme.secondaryColor
        font.pixelSize: Theme.fontSizeSmall
        text: SendParse.formatMoney(Wallet.totalSats, Wallet.btcUsd, Wallet.currency)
    }

    CoverActionList {
        id: actions
        CoverAction {
            iconSource: "image://theme/icon-cover-sync"
            onTriggered: Wallet.sync()
        }
    }
}
