import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Dialog {
    property string amountText: ""
    property string methodName: ""
    property string methodId: ""
    property string destination: ""

    function feeValue() {
        if (!(Wallet.hasPayFee && Wallet.payFeeMethod === methodId))
            return ""
        if (!(Wallet.payFee > 0))
            return "No fee"
        return SendParse.group(Wallet.payFee) + " sats"
    }

    function totalValue() {
        if (!(Wallet.hasPayFee && Wallet.payFeeMethod === methodId))
            return ""
        if (!(Wallet.payFee > 0) || !(Wallet.payGross > Wallet.payNet))
            return ""
        return SendParse.group(Wallet.payGross) + " sats"
    }

    canAccept: true

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            DialogHeader { acceptText: "Pay" }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeExtraLarge
                font.bold: true
                textFormat: Text.RichText
                text: amountText
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.secondaryColor
                text: methodName
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                maximumLineCount: 4
                elide: Text.ElideMiddle
                text: destination
            }
            DetailLine {
                label: "Fee"
                value: feeValue()
            }
            DetailLine {
                label: "Total"
                value: totalValue()
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                color: Theme.secondaryColor
                visible: methodId.length > 0 && feeValue().length === 0 && Wallet.busy
                text: "Estimating fee…"
            }
            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.Wrap
                color: Theme.secondaryColor
                text: "This sends bitcoin from your wallet."
            }
        }
        VerticalScrollDecorator {}
    }
}
