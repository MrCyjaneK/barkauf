import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property bool forwarded: false
    property double rate: Wallet.btcUsd

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
        pageStack.pushAttached(Qt.resolvedUrl("BalancePage.qml"))
        loadRate()
        if (!forwarded) {
            forwarded = true
            pageStack.navigateForward(PageStackAction.Immediate)
        }
    }

    SilicaFlickable {
        anchors.fill: parent
        anchors.bottomMargin: actions.height
        contentHeight: column.height + Theme.paddingLarge

        PullDownMenu {
            MenuItem {
                text: "Copy"
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("SharePage.qml"))
            }
        }

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader {
                title: "Receive"
                description: "Show this code, or copy one payment method."
            }

            Rectangle {
                anchors.horizontalCenter: parent.horizontalCenter
                width: Math.min(parent.width - 4 * Theme.horizontalPageMargin, Theme.itemSizeHuge * 3)
                height: width
                radius: Theme.paddingSmall
                color: "white"
                visible: Wallet.qrImage.length > 0

                Image {
                    anchors.fill: parent
                    anchors.margins: Theme.paddingMedium
                    cache: false
                    fillMode: Image.PreserveAspectFit
                    source: Wallet.qrImage
                }
            }

            Column {
                width: parent.width
                spacing: Theme.paddingSmall
                visible: Wallet.amountSat > 0

                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    font.pixelSize: Theme.fontSizeExtraLarge
                    font.bold: true
                    textFormat: Text.RichText
                    text: SendParse.group(Wallet.amountSat) + " sats"
                }
                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    visible: page.rate > 0 && Wallet.rateCurrency === Wallet.currency
                    color: Theme.secondaryColor
                    font.pixelSize: Theme.fontSizeLarge
                    text: "≈ " + SendParse.formatMoney(Wallet.amountSat, page.rate, Wallet.currency)
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
                color: Theme.highlightColor
                visible: Wallet.error.length > 0
                text: Wallet.error
            }
        }
        VerticalScrollDecorator {}
    }

    ButtonLayout {
        id: actions
        anchors.bottom: parent.bottom
        Button {
            text: Wallet.amountSat > 0 ? "Change amount" : "Add amount"
            onClicked: pageStack.animatorPush(Qt.resolvedUrl("AmountDialog.qml"))
        }
    }
}
