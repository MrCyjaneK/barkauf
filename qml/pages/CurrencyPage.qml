import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property var codes: SendParse.fiatCodes()

    function choose(code) {
        if (code !== Wallet.currency)
            Wallet.setCurrency(code)
        SendParse.loadBtcFiat(code, function (next) {
            if (next > 0)
                Wallet.rememberRate(next)
        })
        pageStack.pop()
    }

    SilicaListView {
        anchors.fill: parent
        model: page.codes
        header: PageHeader {
            title: "Currency"
            description: "Balances and amounts use this currency."
        }
        delegate: ListItem {
            contentHeight: Theme.itemSizeSmall
            onClicked: page.choose(modelData)

            Label {
                x: Theme.horizontalPageMargin
                anchors.verticalCenter: parent.verticalCenter
                color: modelData === Wallet.currency ? Theme.highlightColor : Theme.primaryColor
                text: modelData + " · " + SendParse.fiatInfo(modelData).name
            }
            Label {
                anchors.right: parent.right
                anchors.rightMargin: Theme.horizontalPageMargin
                anchors.verticalCenter: parent.verticalCenter
                color: Theme.secondaryColor
                text: SendParse.fiatInfo(modelData).symbol
            }
        }
        VerticalScrollDecorator {}
    }
}
