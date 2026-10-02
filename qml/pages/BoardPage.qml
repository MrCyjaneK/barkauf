import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property string amountText: ""
    property bool useMax: false
    property bool waiting: false
    property string failure: ""

    function sats() {
        if (useMax)
            return Number(Wallet.onchainConfirmed)
        return parseInt(amountText, 10) || 0
    }
    function belowMin() {
        return sats() > 0 && Number(Wallet.minBoard) > 0 && sats() < Number(Wallet.minBoard)
    }
    function boardBlocked() {
        if (Wallet.boardTxid.length > 0 || waiting)
            return ""
        if (Number(Wallet.onchainConfirmed) <= 0)
            return "Boarding spends confirmed onchain bitcoin. This wallet has none yet."
        if (sats() <= 0)
            return "Enter an amount, or use Max."
        if (sats() > Number(Wallet.onchainConfirmed))
            return "That is more than the confirmed onchain balance."
        return ""
    }
    function canBoard() {
        return sats() > 0
                && sats() <= Number(Wallet.onchainConfirmed)
                && !belowMin()
    }
    function group(n) {
        return SendParse.group(n)
    }

    Timer {
        id: feeTimer
        interval: 300
        onTriggered: {
            var n = page.sats()
            if (n > 0 && !page.belowMin() && n <= Number(Wallet.onchainConfirmed))
                Wallet.estimateBoard(n)
        }
    }

    onAmountTextChanged: if (!waiting) feeTimer.restart()
    onUseMaxChanged: if (!waiting) feeTimer.restart()
    Component.onCompleted: Wallet.loadBoardInfo()

    Connections {
        target: Wallet
        onBusyChanged: {
            if (Wallet.busy || !page.waiting)
                return
            if (Wallet.lastOp !== "board")
                return
            page.waiting = false
            if (Wallet.boardTxid.length === 0)
                page.failure = Wallet.error.length ? Wallet.error : "Boarding failed"
        }
    }

    function startBoard() {
        failure = ""
        waiting = true
        // A full sweep includes every on-chain coin. When some are still
        // unconfirmed, board only the confirmed amount shown on this page.
        Wallet.board(useMax && Number(Wallet.onchainPending) === 0 ? 0 : sats())
    }

    SilicaFlickable {
        anchors.fill: parent
        anchors.bottomMargin: boardButton.height + Theme.paddingLarge
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader {
                title: "Board to Ark"
                description: Wallet.busy && (Wallet.lastOp === "board" || Wallet.lastOp === "ark_info")
                             ? Wallet.activity : ""
            }

            Column {
                width: parent.width
                spacing: Theme.paddingLarge
                visible: Wallet.boardTxid.length === 0

                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.secondaryColor
                    text: "Move onchain bitcoin into Ark for fast, low-cost payments."
                }

                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    color: Theme.secondaryColor
                    font.pixelSize: Theme.fontSizeExtraSmall
                    font.bold: true
                    font.capitalization: Font.AllUppercase
                    text: "Confirmed onchain balance"
                }
                Label {
                    x: Theme.horizontalPageMargin
                    font.pixelSize: Theme.fontSizeExtraLarge
                    font.bold: true
                    textFormat: Text.RichText
                    text: page.group(Wallet.onchainConfirmed) + " sats"
                }

                TextField {
                    id: amountField
                    width: parent.width
                    label: "Amount in sats"
                    placeholderText: "0"
                    inputMethodHints: Qt.ImhDigitsOnly
                    validator: RegExpValidator { regExp: /^[0-9]{0,16}$/ }
                    text: page.amountText
                    EnterKey.enabled: text.length > 0
                    EnterKey.iconSource: "image://theme/icon-m-enter"
                    EnterKey.onClicked: focus = false
                    onTextChanged: {
                        if (page.useMax && text !== String(Wallet.onchainConfirmed))
                            page.useMax = false
                        page.amountText = text
                    }
                }

                Button {
                    anchors.horizontalCenter: parent.horizontalCenter
                    preferredWidth: Theme.buttonWidthSmall
                    text: "Max"
                    enabled: Wallet.onchainConfirmed > 0 && !page.waiting
                    onClicked: {
                        page.useMax = true
                        page.amountText = String(Wallet.onchainConfirmed)
                        amountField.text = page.amountText
                    }
                }

                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.highlightColor
                    visible: page.belowMin()
                    textFormat: Text.RichText
                    text: "The minimum board amount is " + page.group(Wallet.minBoard) + " sats."
                }
                Column {
                    width: parent.width
                    visible: Wallet.hasBoardFee && !page.belowMin()
                    spacing: Theme.paddingSmall
                    DetailLine {
                        label: "Ark balance receives"
                        value: page.group(Wallet.boardNet) + " sats"
                    }
                    DetailLine {
                        label: "Boarding fee"
                        value: page.group(Wallet.boardFee) + " sats"
                    }
                    DetailLine {
                        label: "Amount boarded"
                        value: page.group(Wallet.boardGross) + " sats"
                    }
                }

                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.highlightColor
                    visible: !Wallet.busy && Wallet.lastOp === "board_fee" && !Wallet.hasBoardFee
                             && page.sats() > 0 && !page.belowMin() && Wallet.error.length > 0
                    text: Wallet.error
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.highlightColor
                    visible: page.failure.length > 0
                    text: page.failure
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    wrapMode: Text.Wrap
                    color: Theme.secondaryColor
                    visible: page.boardBlocked().length > 0 && !page.belowMin()
                    text: page.boardBlocked()
                }
            }

            Column {
                width: parent.width
                spacing: Theme.paddingLarge
                visible: Wallet.boardTxid.length > 0

                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    font.pixelSize: Theme.fontSizeExtraLarge
                    font.bold: true
                    text: "Boarding started"
                }
                Label {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    color: Theme.secondaryColor
                    text: "Your onchain bitcoin is moving into Ark. It will become spendable after the board completes."
                }
                DetailLine {
                    label: "Funding transaction"
                    value: Wallet.boardTxid
                    copyable: true
                }
                DetailLine {
                    label: "Explorer"
                    value: /^[0-9a-fA-F]{64}$/.test(Wallet.boardTxid)
                           ? "https://mempool.space/tx/" + Wallet.boardTxid : ""
                    link: true
                }
                Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    visible: Wallet.boardAmount > 0
                    textFormat: Text.RichText
                    text: page.group(Wallet.boardAmount) + " sats"
                }
            }
        }
        VerticalScrollDecorator {}
    }

    Button {
        id: boardButton
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Theme.paddingLarge
        preferredWidth: Theme.buttonWidthLarge
        text: page.waiting ? "Boarding…" : (Wallet.boardTxid.length > 0 ? "Done" : "Board")
        enabled: !page.waiting && (Wallet.boardTxid.length > 0 || page.canBoard())
        onClicked: {
            if (Wallet.boardTxid.length > 0)
                pageStack.pop()
            else
                page.startBoard()
        }
    }
}
