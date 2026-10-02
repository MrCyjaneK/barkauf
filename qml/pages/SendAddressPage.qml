import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page

    property double amountSat: 0
    property string destination: ""
    property double rate: 0

    function group(n) {
        return SendParse.group(n)
    }

    function openMethods() {
        var text = dest.text.trim()
        if (!text.length)
            return
        pageStack.animatorPush(Qt.resolvedUrl("SendMethodPage.qml"), {
            amountSat: amountSat,
            destination: text,
            rate: rate
        })
    }

    function applyText(text) {
        text = String(text || "").replace(/^\s+|\s+$/g, "")
        if (!text.length)
            return
        var parsed = SendParse.parse(text)
        dest.text = parsed.lnurl || text
        if (parsed.amountSat > 0)
            amountSat = parsed.amountSat
        if (parsed.lnurl && !(parsed.amountSat > 0)) {
            var requested = parsed.lnurl
            SendParse.resolveLnurl(requested, function (sats) {
                if (SendParse.parse(dest.text).lnurl !== requested || !(sats > 0))
                    return
                page.amountSat = sats
            })
        }
    }

    function openScan() {
        var obj = pageStack.animatorPush(Qt.resolvedUrl("ScanPage.qml"))
        obj.pageCompleted.connect(function (scanPage) {
            scanPage.accepted.connect(function (text) {
                page.applyText(text)
            })
        })
    }

    SilicaFlickable {
        anchors.fill: parent
        anchors.bottomMargin: continueButton.height + Theme.paddingLarge
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader {
                title: "Who are you paying?"
                description: page.amountSat > 0
                        ? page.group(page.amountSat) + " sats"
                        : "Bitcoin, Lightning, or Ark"
            }

            TextField {
                id: dest
                focus: true
                width: parent.width
                label: "Destination"
                placeholderText: "Address, invoice, or name@domain"
                text: page.destination
                inputMethodHints: Qt.ImhNoPredictiveText | Qt.ImhNoAutoUppercase
                EnterKey.enabled: text.trim().length > 0
                EnterKey.iconSource: "image://theme/icon-m-enter-next"
                EnterKey.onClicked: page.openMethods()
            }

            Row {
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.paddingLarge

                BackgroundItem {
                    width: Theme.buttonWidthSmall
                    height: Theme.itemSizeSmall
                    onClicked: page.applyText(Clipboard.text)
                    Label {
                        anchors.centerIn: parent
                        color: Theme.highlightColor
                        text: "Paste"
                    }
                }
                BackgroundItem {
                    width: Theme.buttonWidthSmall
                    height: Theme.itemSizeSmall
                    onClicked: page.openScan()
                    Label {
                        anchors.centerIn: parent
                        color: Theme.highlightColor
                        text: "Scan"
                    }
                }
            }

        }
        VerticalScrollDecorator {}
    }

    Button {
        id: continueButton
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Theme.paddingLarge
        preferredWidth: Theme.buttonWidthLarge
        text: "Continue"
        enabled: dest.text.trim().length > 0
        onClicked: page.openMethods()
    }
}
