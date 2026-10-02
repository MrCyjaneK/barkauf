import QtQuick 2.0
import Sailfish.Silica 1.0

Page {
    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader { title: "Details" }

            SectionHeader { text: "Onchain" }
            Repeater {
                model: [
                    { label: "Total", value: Wallet.onchainTotal },
                    { label: "Confirmed", value: Wallet.onchainConfirmed },
                    { label: "Pending", value: Wallet.onchainPending }
                ]
                delegate: Item {
                    x: Theme.horizontalPageMargin
                    width: column.width - 2 * x
                    height: nameLabel.height + Theme.paddingSmall
                    Label {
                        id: nameLabel
                        anchors.verticalCenter: parent.verticalCenter
                        text: modelData.label
                    }
                    Label {
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        color: Theme.highlightColor
                        text: modelData.value + " sats"
                    }
                }
            }

            SectionHeader { text: "Offchain" }
            Repeater {
                model: [
                    { label: "Total", value: Wallet.offchainTotal },
                    { label: "Spendable", value: Wallet.spendableSats },
                    { label: "Pending send", value: Wallet.pendingSend },
                    { label: "Pending in round", value: Wallet.pendingInRound },
                    { label: "Pending exit", value: Wallet.pendingExit },
                    { label: "Pending board", value: Wallet.pendingBoard }
                ]
                delegate: Item {
                    x: Theme.horizontalPageMargin
                    width: column.width - 2 * x
                    height: nameLabel.height + Theme.paddingSmall
                    Label {
                        id: nameLabel
                        anchors.verticalCenter: parent.verticalCenter
                        text: modelData.label
                    }
                    Label {
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        color: Theme.highlightColor
                        text: modelData.value + " sats"
                    }
                }
            }
        }
        VerticalScrollDecorator {}
    }
}
