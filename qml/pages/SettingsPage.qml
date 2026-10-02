import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader { title: "Settings" }

            SectionHeader { text: "Account" }
            SettingRow {
                title: "Profile"
                description: "Name and the keys this wallet can share."
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("ProfilePage.qml"))
            }
            SettingRow {
                title: "Currency"
                subtitle: Wallet.currency + " · " + SendParse.fiatInfo(Wallet.currency).name
                description: "Choose the fiat currency used for balances and payment amounts."
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("CurrencyPage.qml"))
            }

            SectionHeader { text: "Wallet" }
            SettingRow {
                title: "Show Seed Phrase"
                description: "Never share your seed phrase with anyone. It is important to keep it safe and secure."
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("SeedPage.qml"))
            }
            SettingRow {
                title: "Show VTXOs"
                description: "VTXOs are to Ark like UTXOs are to Bitcoin"
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("VtxosPage.qml"))
            }
            SettingRow {
                title: "Board to Ark"
                description: "Manually move onchain bitcoin into Ark."
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("BoardPage.qml"))
            }

            SectionHeader { text: "Data" }
            BackgroundItem {
                width: parent.width
                height: wipeContent.implicitHeight + Theme.paddingLarge
                onClicked: Remorse.popupAction(page, "Wiping wallet", function() {
                    Wallet.wipe()
                    Qt.quit()
                })

                Column {
                    id: wipeContent
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Theme.paddingSmall / 2

                    Label {
                        width: parent.width
                        text: "Wipe all data"
                        truncationMode: TruncationMode.Fade
                    }
                    Label {
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: Theme.secondaryColor
                        font.pixelSize: Theme.fontSizeSmall
                        text: "Delete the wallet stored on this device and quit."
                    }
                }
            }
        }
        VerticalScrollDecorator {}
    }
}
