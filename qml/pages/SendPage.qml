import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page

    property bool usdMode: false
    property bool updating: false
    property double rate: Wallet.btcUsd
    property double amountSat: 0
    property string destination: ""
    property string priceError: ""
    property double available: Wallet.spendableSats + Wallet.onchainConfirmed

    function group(n) {
        return SendParse.group(n)
    }

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

    function setMax() {
        if (usdMode && !(rate > 0)) {
            priceError = "Price unavailable"
            return
        }
        showAmount(available)
    }

    function openAddress() {
        pageStack.animatorPush(Qt.resolvedUrl("SendAddressPage.qml"), {
            amountSat: amountSat,
            destination: destination,
            rate: rate
        })
    }

    function openScan() {
        var obj = pageStack.animatorPush(Qt.resolvedUrl("ScanPage.qml"))
        obj.pageCompleted.connect(function (scanPage) {
            scanPage.accepted.connect(function (text) {
                page.applyParsed(SendParse.parse(text))
            })
        })
    }

    function openNfc() {
        pageStack.animatorPush(Qt.resolvedUrl("NfcPage.qml"))
    }

    function pasteAmount() {
        applyParsed(SendParse.parse(Clipboard.text))
    }

    function takeLnurlAmount() {
        if (!destination || !Wallet.lnurlReady || Wallet.lnurlTarget !== destination)
            return
        if (Wallet.lnurlFixed && Wallet.lnurlAmount > 0)
            showAmount(Wallet.lnurlAmount)
    }

    function applyParsed(parsed) {
        if (parsed.lnurl && !(parsed.amountSat > 0)) {
            destination = parsed.lnurl
            Wallet.lookupLnurl(parsed.lnurl)
            page.takeLnurlAmount()
            return
        }
        if (parsed.destination)
            destination = parsed.destination
        if (parsed.amountSat >= 0) {
            showAmount(parsed.amountSat)
            return
        }
        if (parsed.unit === "usd") {
            if (!(rate > 0)) {
                priceError = "Price unavailable"
                return
            }
            usdMode = true
            showAmount(SendParse.satsFromUSD(parsed.rawNumber, rate))
            return
        }
        if (parsed.rawNumber) {
            updating = true
            amountField.text = parsed.rawNumber
            updating = false
            amountSat = usdMode
                    ? SendParse.satsFromUSD(parsed.rawNumber, rate)
                    : (parseInt(parsed.rawNumber, 10) || 0)
        }
    }

    function convertedText() {
        if (usdMode)
            return "≈ " + group(amountSat) + " sats"
        if (rate > 0)
            return "≈ " + SendParse.formatMoney(amountSat, rate, Wallet.currency)
        return priceError
    }

    Connections {
        target: Wallet
        onChanged: page.takeLnurlAmount()
    }

    onStatusChanged: {
        if (status !== PageStatus.Active)
            return
        if (!(rate > 0))
            loadRate()
        if (!Wallet.busy)
            Wallet.sync()
        amountField.forceActiveFocus()
    }

    SilicaFlickable {
        anchors.fill: parent
        anchors.bottomMargin: actions.height
        contentHeight: column.height + Theme.paddingLarge

        PullDownMenu {
            MenuItem {
                text: "Scan"
                onClicked: page.openScan()
            }
            MenuItem {
                text: "NFC"
                onClicked: page.openNfc()
            }
            MenuItem {
                text: "Paste"
                onClicked: page.pasteAmount()
            }
            MenuItem {
                text: "Max"
                onClicked: page.setMax()
            }
        }

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            PageHeader {
                title: "Send"
                description: page.group(page.available) + " sats available"
            }

            Item {
                width: parent.width
                height: amountRow.height + Theme.paddingLarge

                Row {
                    id: amountRow
                    anchors.centerIn: parent
                    spacing: Theme.paddingMedium

                    TextInput {
                        id: amountField
                        width: Math.max(implicitWidth + Theme.paddingLarge, Theme.itemSizeHuge)
                        height: implicitHeight
                        horizontalAlignment: Text.AlignHCenter
                        font.pixelSize: text.length > 9 ? Theme.fontSizeExtraLarge : Theme.fontSizeHuge
                        font.bold: true
                        color: Theme.primaryColor
                        selectionColor: Theme.secondaryHighlightColor
                        selectedTextColor: Theme.highlightColor
                        inputMethodHints: page.usdMode ? Qt.ImhFormattedNumbersOnly : Qt.ImhDigitsOnly
                        validator: RegExpValidator {
                            regExp: page.usdMode
                                   ? (SendParse.fiatDecimals(Wallet.currency) === 0
                                      ? /^[0-9]{0,12}$/
                                      : /^[0-9]*\.?[0-9]{0,2}$/)
                                   : /^[0-9]{0,16}$/
                        }
                        Keys.onReturnPressed: page.openAddress()
                        Keys.onEnterPressed: page.openAddress()
                        onTextChanged: {
                            if (page.updating)
                                return
                            page.amountSat = page.usdMode
                                    ? SendParse.satsFromUSD(text, page.rate)
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
                        text: page.usdMode ? Wallet.currency : "sats"
                    }
                }
            }

            BackgroundItem {
                width: parent.width
                height: Theme.itemSizeSmall
                onClicked: page.toggleUnit()

                Label {
                    anchors.centerIn: parent
                    color: page.rate > 0 || page.usdMode ? Theme.highlightColor : Theme.secondaryColor
                    font.pixelSize: Theme.fontSizeLarge
                    textFormat: Text.RichText
                    text: page.convertedText()
                }
            }

        }
        VerticalScrollDecorator {}
    }

    ButtonLayout {
        id: actions
        anchors.bottom: parent.bottom
        Button {
            text: "Next"
            enabled: page.amountSat > 0
            onClicked: page.openAddress()
        }
    }
}
