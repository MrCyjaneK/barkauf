import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page
    property string copied: ""

    function copyText(text) {
        if (!text || !text.length)
            return
        Clipboard.text = text
        copied = text
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader {
                title: "Copy"
                description: Wallet.amountSat > 0
                             ? SendParse.group(Wallet.amountSat) + " sats"
                             : "Payment details"
            }

            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                enabled: Wallet.canAll
                opacity: enabled ? 1.0 : Theme.opacityLow
                onClicked: page.copyText(Wallet.bip321)
                Row {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    height: parent.height
                    spacing: Theme.paddingMedium
                    RailIcon {
                        width: Theme.iconSizeSmall
                        height: width
                        anchors.verticalCenter: parent.verticalCenter
                        rail: "request"
                        color: parent.parent.enabled ? Theme.primaryColor : Theme.secondaryColor
                    }
                    Column {
                        width: parent.width - Theme.iconSizeSmall - parent.spacing
                        anchors.verticalCenter: parent.verticalCenter
                        Label { text: "Payment request" }
                        Label {
                            width: parent.width
                            color: Theme.secondaryColor
                            font.pixelSize: Theme.fontSizeSmall
                            elide: Text.ElideMiddle
                            text: Wallet.bip321
                        }
                    }
                }
            }
            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                enabled: Wallet.canArk
                opacity: enabled ? 1.0 : Theme.opacityLow
                onClicked: page.copyText(Wallet.ark)
                Row {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    height: parent.height
                    spacing: Theme.paddingMedium
                    RailIcon {
                        width: Theme.iconSizeSmall
                        height: width
                        anchors.verticalCenter: parent.verticalCenter
                        rail: "ark"
                        color: parent.parent.enabled ? Theme.primaryColor : Theme.secondaryColor
                    }
                    Column {
                        width: parent.width - Theme.iconSizeSmall - parent.spacing
                        anchors.verticalCenter: parent.verticalCenter
                        Label { text: "Ark" }
                        Label {
                            width: parent.width
                            color: Theme.secondaryColor
                            font.pixelSize: Theme.fontSizeSmall
                            elide: Text.ElideMiddle
                            text: Wallet.ark
                        }
                    }
                }
            }
            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                enabled: Wallet.canLightning
                opacity: enabled ? 1.0 : Theme.opacityLow
                onClicked: page.copyText(Wallet.bolt11)
                Row {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    height: parent.height
                    spacing: Theme.paddingMedium
                    RailIcon {
                        width: Theme.iconSizeSmall
                        height: width
                        anchors.verticalCenter: parent.verticalCenter
                        rail: "lightning"
                        color: parent.parent.enabled ? Theme.primaryColor : Theme.secondaryColor
                    }
                    Column {
                        width: parent.width - Theme.iconSizeSmall - parent.spacing
                        anchors.verticalCenter: parent.verticalCenter
                        Label { text: "Lightning" }
                        Label {
                            width: parent.width
                            color: Theme.secondaryColor
                            font.pixelSize: Theme.fontSizeSmall
                            elide: Text.ElideMiddle
                            text: Wallet.bolt11
                        }
                    }
                }
            }
            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                enabled: Wallet.canOnchain
                opacity: enabled ? 1.0 : Theme.opacityLow
                onClicked: page.copyText(Wallet.onchain)
                Row {
                    x: Theme.horizontalPageMargin
                    width: parent.width - 2 * x
                    height: parent.height
                    spacing: Theme.paddingMedium
                    RailIcon {
                        width: Theme.iconSizeSmall
                        height: width
                        anchors.verticalCenter: parent.verticalCenter
                        rail: "onchain"
                        color: parent.parent.enabled ? Theme.primaryColor : Theme.secondaryColor
                    }
                    Column {
                        width: parent.width - Theme.iconSizeSmall - parent.spacing
                        anchors.verticalCenter: parent.verticalCenter
                        Label { text: "On-chain" }
                        Label {
                            width: parent.width
                            color: Theme.secondaryColor
                            font.pixelSize: Theme.fontSizeSmall
                            elide: Text.ElideMiddle
                            text: Wallet.onchain
                        }
                    }
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                color: Theme.highlightColor
                visible: page.copied.length > 0
                text: "Copied\n" + page.copied
            }
        }
        VerticalScrollDecorator {}
    }
}
