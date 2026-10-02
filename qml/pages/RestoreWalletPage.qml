import QtQuick 2.0
import Sailfish.Silica 1.0

Page {
    id: page
    property string errorText: ""
    property bool wantImport: false

    function showBalance() {
        if (pageStack.depth > 1)
            pageStack.pop(undefined, PageStackAction.Immediate)
        pageStack.replace(Qt.resolvedUrl("ReceivePage.qml"), {}, PageStackAction.Immediate)
    }

    Connections {
        target: Wallet
        onChanged: {
            if (!wantImport || Wallet.lastOp !== "import")
                return
            wantImport = false
            if (Wallet.hasWallet)
                openBalanceTimer.start()
            else
                errorText = Wallet.error.length ? Wallet.error : "Could not restore the wallet"
        }
    }

    Timer {
        id: openBalanceTimer
        interval: 1
        onTriggered: page.showBalance()
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader { title: "Restore wallet" }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                text: "Enter your 12-word recovery phrase."
            }

            TextArea {
                id: seedField
                width: parent.width
                focus: true
                label: "Recovery phrase"
                placeholderText: "twelve words"
                wrapMode: TextEdit.Wrap
                enabled: !wantImport
            }

            BusyIndicator {
                anchors.horizontalCenter: parent.horizontalCenter
                size: BusyIndicatorSize.Medium
                running: wantImport
                visible: running
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                visible: wantImport && Wallet.activity.length > 0
                text: Wallet.activity
            }

            Button {
                enabled: seedField.text.trim().length > 0 && !wantImport && !Wallet.busy
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                anchors.horizontalCenter: parent.horizontalCenter
                text: "Continue"
                onClicked: {
                    errorText = ""
                    wantImport = true
                    Wallet.importMnemonic(seedField.text)
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: errorText.length > 0
                text: errorText
            }
        }
        VerticalScrollDecorator {}
    }
}
