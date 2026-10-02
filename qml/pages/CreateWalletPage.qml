import QtQuick 2.0
import Sailfish.Silica 1.0

Page {
    id: page
    property string phrase: ""
    property string errorText: ""
    property bool wantPhrase: false
    property bool wantImport: false
    property bool copied: false
    property var words: phrase.length ? phrase.split(" ") : []

    function requestPhrase() {
        errorText = ""
        copied = false
        wantPhrase = true
        Wallet.generate()
    }

    function showBalance() {
        if (pageStack.depth > 1)
            pageStack.pop(undefined, PageStackAction.Immediate)
        pageStack.replace(Qt.resolvedUrl("ReceivePage.qml"), {}, PageStackAction.Immediate)
    }

    Component.onCompleted: requestPhrase()

    Connections {
        target: Wallet
        onChanged: {
            if (wantPhrase && Wallet.lastOp === "generate") {
                wantPhrase = false
                if (Wallet.mnemonic.length > 0)
                    phrase = Wallet.mnemonic
                else
                    errorText = Wallet.error.length ? Wallet.error : "Could not create a recovery phrase"
                return
            }
            if (wantImport && Wallet.lastOp === "import") {
                wantImport = false
                if (Wallet.hasWallet)
                    openBalanceTimer.start()
                else
                    errorText = Wallet.error.length ? Wallet.error : "Could not create the wallet"
            }
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

            PageHeader { title: "Your recovery phrase" }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                text: "Write down these 12 words in order and store them in a safe place. This is the only way to recover your wallet."
            }

            BusyIndicator {
                anchors.horizontalCenter: parent.horizontalCenter
                size: BusyIndicatorSize.Medium
                running: wantPhrase || wantImport
                visible: running
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                visible: (wantPhrase || wantImport) && Wallet.activity.length > 0
                text: Wallet.activity
            }

            Grid {
                id: wordGrid
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                columns: 2
                visible: words.length > 0
                rowSpacing: Theme.paddingMedium
                columnSpacing: Theme.paddingLarge

                Repeater {
                    model: words
                    delegate: Item {
                        width: (wordGrid.width - wordGrid.columnSpacing) / 2
                        height: wordLabel.height

                        Label {
                            id: indexLabel
                            width: Math.round(Theme.fontSizeMedium * 1.6)
                            text: (index + 1) + "."
                            color: Theme.secondaryColor
                            horizontalAlignment: Text.AlignRight
                        }
                        Label {
                            id: wordLabel
                            anchors.left: indexLabel.right
                            anchors.leftMargin: Theme.paddingSmall
                            anchors.right: parent.right
                            text: modelData
                            font.pixelSize: Theme.fontSizeLarge
                            elide: Text.ElideRight
                        }
                    }
                }
            }

            Button {
                visible: words.length === 12
                enabled: !wantImport
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                anchors.horizontalCenter: parent.horizontalCenter
                text: copied ? "Copied" : "Copy seed phrase"
                onClicked: {
                    Clipboard.text = phrase
                    copied = true
                }
            }
            Button {
                visible: words.length === 12
                enabled: !wantImport && !Wallet.busy
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                anchors.horizontalCenter: parent.horizontalCenter
                text: "I have saved it, Continue"
                onClicked: {
                    errorText = ""
                    wantImport = true
                    Wallet.importMnemonic(phrase)
                }
            }
            Button {
                visible: errorText.length > 0 && words.length === 0
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                anchors.horizontalCenter: parent.horizontalCenter
                text: "Try again"
                onClicked: requestPhrase()
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
