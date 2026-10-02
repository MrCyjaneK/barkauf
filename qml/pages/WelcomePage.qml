import QtQuick 2.0
import Sailfish.Silica 1.0

Page {
    id: welcome
    property bool replaced: false

    function considerBalance() {
        if (replaced || !Wallet.ready || !Wallet.hasWallet)
            return
        if (pageStack.currentPage !== welcome)
            return
        replaced = true
        openBalanceTimer.start()
    }

    Component.onCompleted: considerBalance()

    Connections {
        target: Wallet
        onChanged: welcome.considerBalance()
    }

    Timer {
        id: openBalanceTimer
        interval: 1
        onTriggered: pageStack.replace(Qt.resolvedUrl("ReceivePage.qml"), {}, PageStackAction.Immediate)
    }

    Column {
        anchors.centerIn: parent
        width: parent.width - 2 * Theme.horizontalPageMargin
        spacing: Theme.paddingLarge
        visible: !Wallet.ready

        BusyIndicator {
            anchors.horizontalCenter: parent.horizontalCenter
            size: BusyIndicatorSize.Large
            running: parent.visible
        }
        Label {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            color: Theme.secondaryColor
            text: Wallet.activity.length > 0 ? Wallet.activity : "Starting wallet"
        }
    }

    SilicaFlickable {
        anchors.fill: parent
        visible: Wallet.ready && !Wallet.hasWallet
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader { title: "Welcome to Barkauf" }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                font.pixelSize: Theme.fontSizeMedium
                text: "Create a new wallet or restore an existing one."
            }

            Button {
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                anchors.horizontalCenter: parent.horizontalCenter
                text: "Create wallet"
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("CreateWalletPage.qml"))
            }
            Button {
                preferredWidth: parent.width - 2 * Theme.horizontalPageMargin
                anchors.horizontalCenter: parent.horizontalCenter
                text: "Restore wallet"
                onClicked: pageStack.animatorPush(Qt.resolvedUrl("RestoreWalletPage.qml"))
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: Wallet.error.length > 0
                text: Wallet.error
            }
        }
        VerticalScrollDecorator {}
    }
}
