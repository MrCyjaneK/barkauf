import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Dialog {
    id: dialog

    canAccept: true

    property bool usdMode: false
    property bool updating: false
    property double rate: Wallet.btcUsd
    property double amountSat: 0
    property string priceError: ""

    function loadRate() {
        if (Wallet.rateCurrency === Wallet.currency && Wallet.btcUsd > 0)
            rate = Wallet.btcUsd
        else
            rate = 0
        SendParse.loadBtcFiat(Wallet.currency, function (next) {
            if (next > 0) {
                rate = next
                priceError = ""
                Wallet.rememberRate(next)
                if (usdMode)
                    showAmount(amountSat)
            } else if (!(rate > 0)) {
                priceError = "Price unavailable"
            }
        })
    }

    function showAmount(sats) {
        updating = true
        amountSat = sats > 0 ? sats : 0
        if (amountSat === 0)
            amountField.text = ""
        else if (usdMode)
            amountField.text = SendParse.formatFiat(amountSat, rate, Wallet.currency)
        else
            amountField.text = String(Math.round(amountSat))
        updating = false
    }

    function toggleUnit() {
        if (!usdMode && !(rate > 0)) {
            priceError = "Price unavailable"
            return
        }
        usdMode = !usdMode
        showAmount(amountSat)
        amountField.forceActiveFocus()
    }

    function convertedText() {
        if (usdMode)
            return "≈ " + SendParse.group(amountSat) + " sats"
        if (rate > 0)
            return "≈ " + SendParse.formatMoney(amountSat, rate, Wallet.currency)
        return priceError
    }

    onStatusChanged: {
        if (status !== DialogStatus.Opened)
            return
        loadRate()
        showAmount(Wallet.amountSat)
        amountField.forceActiveFocus()
    }

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            DialogHeader { acceptText: "Set" }

            Item {
                width: parent.width
                height: amountRow.height

                Row {
                    id: amountRow
                    anchors.centerIn: parent
                    spacing: Theme.paddingMedium

                    TextInput {
                        id: amountField
                        focus: true
                        width: Math.max(implicitWidth + Theme.paddingLarge, Theme.itemSizeHuge)
                        height: implicitHeight
                        horizontalAlignment: Text.AlignHCenter
                        font.pixelSize: text.length > 9 ? Theme.fontSizeExtraLarge : Theme.fontSizeHuge
                        font.bold: true
                        color: Theme.primaryColor
                        selectionColor: Theme.secondaryHighlightColor
                        selectedTextColor: Theme.highlightColor
                        inputMethodHints: dialog.usdMode ? Qt.ImhFormattedNumbersOnly : Qt.ImhDigitsOnly
                        validator: RegExpValidator {
                            regExp: dialog.usdMode
                                   ? (SendParse.fiatDecimals(Wallet.currency) === 0
                                      ? /^[0-9]{0,12}$/
                                      : /^[0-9]*\.?[0-9]{0,2}$/)
                                   : /^[0-9]{0,16}$/
                        }
                        Keys.onReturnPressed: dialog.accept()
                        Keys.onEnterPressed: dialog.accept()
                        onTextChanged: {
                            if (dialog.updating)
                                return
                            dialog.amountSat = dialog.usdMode
                                    ? SendParse.satsFromUSD(text, dialog.rate)
                                    : (parseInt(text, 10) || 0)
                        }

                        Label {
                            anchors.centerIn: parent
                            visible: amountField.text.length === 0
                            font.pixelSize: amountField.font.pixelSize
                            font.bold: true
                            color: Theme.secondaryColor
                            text: "0"
                        }
                    }

                    Label {
                        anchors.baseline: amountField.baseline
                        font.pixelSize: Theme.fontSizeLarge
                        color: Theme.secondaryColor
                        text: dialog.usdMode ? Wallet.currency : "sats"
                    }
                }
            }

            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                onClicked: dialog.toggleUnit()

                Label {
                    anchors.centerIn: parent
                    color: dialog.rate > 0 || dialog.usdMode ? Theme.highlightColor : Theme.secondaryColor
                    font.pixelSize: Theme.fontSizeLarge
                    textFormat: Text.RichText
                    text: dialog.convertedText()
                }
            }
        }
        VerticalScrollDecorator {}
    }

    onAccepted: Wallet.setAmount(dialog.amountSat)
}
