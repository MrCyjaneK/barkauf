import QtQuick 2.0
import Sailfish.Silica 1.0

Page {
    id: page
    property string draft: Wallet.displayName

    Component.onCompleted: nameField.text = Wallet.displayName

    function saveName() {
        Wallet.setDisplayName(draft)
        nameField.focus = false
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader {
                title: "Profile"
                description: "This name stays on this phone."
            }

            TextField {
                id: nameField
                width: parent.width
                label: "Name"
                placeholderText: "Add a display name"
                EnterKey.enabled: true
                EnterKey.iconSource: "image://theme/icon-m-acknowledge"
                EnterKey.onClicked: page.saveName()
                onTextChanged: page.draft = text
            }

            Button {
                anchors.horizontalCenter: parent.horizontalCenter
                preferredWidth: Theme.buttonWidthSmall
                text: "Save"
                onClicked: page.saveName()
            }

            SectionHeader { text: "Wallet" }
            DetailLine {
                label: "Fingerprint"
                value: Wallet.fingerprint
                copyable: true
            }
            DetailLine {
                label: "Ark address"
                value: Wallet.ark
                copyable: true
            }
            DetailLine {
                label: "On-chain address"
                value: Wallet.onchain
                copyable: true
            }
        }
        VerticalScrollDecorator {}
    }
}
