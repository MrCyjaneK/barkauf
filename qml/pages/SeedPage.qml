import QtQuick 2.0
import Sailfish.Silica 1.0

Page {
    id: page
    property string phrase: Wallet.mnemonic
    property var words: phrase.length ? phrase.split(" ") : []
    property bool copied: false

    Timer {
        id: copyTimer
        interval: 1200
        onTriggered: page.copied = false
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
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                text: "Never share your seed phrase with anyone."
            }

            Grid {
                id: wordGrid
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                columns: 2
                visible: page.words.length > 0
                rowSpacing: Theme.paddingMedium
                columnSpacing: Theme.paddingLarge

                Repeater {
                    model: page.words
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

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                visible: page.words.length === 0
                text: Wallet.busy ? Wallet.activity : "Recovery phrase is not available yet."
            }

            Button {
                visible: page.words.length > 0
                anchors.horizontalCenter: parent.horizontalCenter
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                text: page.copied ? "Copied" : "Copy seed phrase"
                onClicked: {
                    Clipboard.text = page.phrase
                    page.copied = true
                    copyTimer.restart()
                }
            }
        }
        VerticalScrollDecorator {}
    }
}
