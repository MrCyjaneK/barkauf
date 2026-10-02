import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property string offered: ""

    onStatusChanged: {
        if (status === PageStatus.Active)
            Nfc.start()
        else if (status === PageStatus.Inactive)
            Nfc.stop()
    }

    function offer(text) {
        text = String(text || "").replace(/^\s+|\s+$/g, "")
        if (!text.length || text === offered)
            return
        offered = text
        var parsed = SendParse.parse(text)
        pageStack.animatorPush(Qt.resolvedUrl("SendMethodPage.qml"), {
            destination: text,
            amountSat: parsed.amountSat > 0 ? parsed.amountSat : 0,
            rate: Wallet.btcUsd
        })
    }

    Connections {
        target: Nfc
        onTextRead: page.offer(text)
        onChanged: {
            if (page.status === PageStatus.Active && Nfc.statusText === "waiting for a tag")
                page.offered = ""
        }
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingMedium

            PageHeader { title: "NFC" }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * Theme.horizontalPageMargin
                wrapMode: Text.Wrap
                text: Nfc.statusText
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * Theme.horizontalPageMargin
                wrapMode: Text.Wrap
                text: "Enabled: " + (Nfc.enabled ? "yes" : "no")
                      + "  Powered: " + (Nfc.powered ? "yes" : "no")
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * Theme.horizontalPageMargin
                wrapMode: Text.Wrap
                text: "Sharing: " + Wallet.bip321
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * Theme.horizontalPageMargin
                wrapMode: Text.Wrap
                text: "Last read: " + Nfc.lastRead
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * Theme.horizontalPageMargin
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                text: Wallet.payResult.length ? Wallet.payResult : Wallet.error
            }

            Button {
                x: Theme.horizontalPageMargin
                text: Nfc.sharing ? "Stop sharing" : "Share payment URI"
                onClicked: {
                    if (Nfc.sharing) {
                        Nfc.setSharing(false)
                    } else {
                        Nfc.shareText = Wallet.bip321
                        Nfc.setSharing(true)
                    }
                }
            }
        }
        VerticalScrollDecorator {}
    }
}
