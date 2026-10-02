import QtQuick 2.0
import Sailfish.Silica 1.0
import "../sendparse.js" as SendParse

Page {
    id: page

    property double amountSat: 0
    property string destination: ""
    property double rate: 0
    property var parsed: SendParse.parse(destination)
    property string chosen: ""
    property bool paying: false
    property bool waiting: false
    property string failure: ""
    property bool feePending: false
    property string feeAsked: ""
    property var methods: [
        { rail: "ark", title: "Ark" },
        { rail: "lightning", title: "Lightning" },
        { rail: "onchain", title: "On-chain" }
    ]

    Timer {
        id: feeTimer
        interval: 300
        onTriggered: page.refreshFee()
    }

    onChosenChanged: page.scheduleFee()
    Component.onCompleted: {
        page.considerLnurl()
        page.scheduleFee()
    }

    function considerLnurl() {
        if (!parsed.lnurl)
            return
        Wallet.lookupLnurl(parsed.lnurl)
        page.takeLnurlAmount()
    }

    function takeLnurlAmount() {
        if (!parsed.lnurl || !Wallet.lnurlReady || Wallet.lnurlTarget !== parsed.lnurl)
            return
        if (!(Wallet.lnurlFixed && Wallet.lnurlAmount > 0) || amountSat === Wallet.lnurlAmount)
            return
        amountSat = Wallet.lnurlAmount
        page.scheduleFee()
    }

    function payTo() {
        var method = activeMethod()
        if (method === "ark")
            return String(parsed.ark || "")
        if (method === "lightning")
            return String(parsed.lnAddress || parsed.lnurl || parsed.lightning || "")
        if (method === "onchain")
            return String(parsed.onchain || "")
        return destination
    }

    function askConfirm() {
        if (waiting || Wallet.busy || !activeMethod().length)
            return
        failure = ""
        var dialog = pageStack.push(Qt.resolvedUrl("PayConfirmDialog.qml"), {
            amountText: group(shownAmount()) + " sats",
            methodName: methodTitle(activeMethod()),
            methodId: activeMethod(),
            destination: payTo()
        })
        dialog.accepted.connect(function() { page.startPay() })
    }

    function startPay() {
        if (waiting || Wallet.busy)
            return
        failure = ""
        pay()
    }

    function finishPay() {
        if (!waiting || Wallet.lastOp !== "pay")
            return
        waiting = false
        paying = false
        if (Wallet.error.length > 0) {
            failure = Wallet.error
            return
        }
        // Accepting a dialog navigates forward. Send is attached in front of
        // Balance, so the default forward move returns to the sending screen.
        // Pop to Balance instead.
        var home = pageStack.find(function (candidate) {
            return candidate.objectName === "balance"
        })
        pageStack.push(Qt.resolvedUrl("PaySentDialog.qml"), {
            amountText: group(shownAmount()) + " sats",
            methodName: methodTitle(activeMethod()),
            acceptDestination: home,
            acceptDestinationAction: PageStackAction.Pop
        })
    }

    function group(n) {
        return SendParse.group(n)
    }

    function shownAmount() {
        if (amountSat > 0)
            return amountSat
        var fixed = invoiceSats()
        return fixed > 0 ? fixed : 0
    }

    function invoiceSats() {
        if (!parsed.lightning)
            return -1
        return SendParse.bolt11Amount(parsed.lightning)
    }

    function arkOk() {
        return parsed.ark.length > 0 && amountSat > 0
    }

    function lightningOk() {
        if (parsed.lnurl || parsed.lnAddress)
            return amountSat > 0
        if (!parsed.lightning)
            return false
        var fixed = invoiceSats()
        if (fixed >= 0)
            return fixed > 0
        return amountSat > 0
    }

    function onchainOk() {
        return parsed.onchain.length > 0 && amountSat > 0
    }

    function methodOk(name) {
        if (name === "ark")
            return arkOk()
        if (name === "lightning")
            return lightningOk()
        if (name === "onchain")
            return onchainOk()
        return false
    }

    function recommended() {
        if (arkOk())
            return "ark"
        if (lightningOk())
            return "lightning"
        if (onchainOk())
            return "onchain"
        return ""
    }

    function activeMethod() {
        if (methodOk(chosen))
            return chosen
        return recommended()
    }

    function methodTitle(name) {
        if (name === "ark")
            return "Ark"
        if (name === "lightning")
            return "Lightning"
        if (name === "onchain")
            return "On-chain"
        return "Pay"
    }

    function methodAddress(name) {
        if (name === "ark")
            return String(parsed.ark || "")
        if (name === "lightning")
            return String(parsed.lnAddress || parsed.lnurl || parsed.lightning || "")
        if (name === "onchain")
            return String(parsed.onchain || "")
        return ""
    }

    function estimatedSats() {
        if (activeMethod() === "lightning" && invoiceSats() > 0)
            return invoiceSats()
        return amountSat
    }

    function scheduleFee() {
        var method = activeMethod()
        feeAsked = method
        feePending = method.length > 0
        if (!waiting)
            feeTimer.restart()
    }

    function refreshFee() {
        var method = activeMethod()
        feeAsked = method
        if (!method.length || waiting) {
            feePending = false
            return
        }
        var sats = estimatedSats()
        if (!(sats > 0)) {
            feePending = false
            return
        }
        feePending = true
        Wallet.estimatePay(method, payTo(), sats)
    }

    function feeText() {
        if (!(Wallet.payFee > 0))
            return "No fee"
        var text = "Fee " + group(Wallet.payFee) + " sats"
        if (Wallet.payGross > 0 && Wallet.payGross !== estimatedSats())
            text += " · total " + group(Wallet.payGross) + " sats"
        return text
    }

    function extraDetail(name) {
        if (!methodOk(name)) {
            if (name === "lightning" && parsed.lnurl && !(amountSat > 0)
                    && Wallet.lnurlTarget === parsed.lnurl && !Wallet.lnurlReady)
                return "Reading amount…"
            return "Not in this request"
        }
        var parts = []
        if (recommended() === name)
            parts.push("Recommended")
        if (name === "lightning") {
            var fixed = invoiceSats()
            if (fixed >= 0 && fixed !== amountSat)
                parts.push("Invoice is " + group(fixed) + " sats")
        }
        if (name === activeMethod()) {
            if (feePending)
                parts.push("Estimating fee…")
            else if (Wallet.hasPayFee && Wallet.payFeeMethod === name)
                parts.push(feeText())
            else if (Wallet.lastOp === "pay_fee" && Wallet.payFeeMethod === name && Wallet.error.length > 0)
                parts.push(Wallet.error)
            else if (name === "onchain" && Wallet.lastOp === "pay_fee" && Wallet.payFeeMethod === name
                     && Number(Wallet.onchainConfirmed) > page.estimatedSats())
                parts.push("Miner fee comes out of the confirmed balance")
        }
        return parts.join(" · ")
    }

    Connections {
        target: Wallet
        onChanged: {
            page.takeLnurlAmount()
            page.finishPay()
        }
        onBusyChanged: {
            if (Wallet.busy)
                return
            if (Wallet.lastOp === "pay_fee" && Wallet.payFeeMethod === page.feeAsked)
                page.feePending = false
        }
    }

    function pay() {
        var method = activeMethod()
        if (!method.length || Wallet.busy)
            return
        var sats = method === "lightning" && invoiceSats() > 0 ? invoiceSats() : amountSat
        Wallet.pay(SendParse.payUri(method, parsed, sats))
        // The helper emits changed while queueing. Arm only after that, so a
        // previous pay result cannot be reported as this one.
        if (!Wallet.busy) {
            failure = Wallet.error.length ? Wallet.error : "Payment could not be started"
            return
        }
        waiting = true
        paying = true
    }

    SilicaFlickable {
        anchors.fill: parent
        anchors.bottomMargin: payButton.height + Theme.paddingLarge
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingSmall

            PageHeader {
                title: "Pay with"
                description: "The first available method is selected."
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeExtraLarge
                font.bold: true
                textFormat: Text.RichText
                text: page.group(page.shownAmount()) + " sats"
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.secondaryColor
                visible: page.rate > 0 && Wallet.rateCurrency === Wallet.currency
                text: SendParse.formatMoney(page.shownAmount(), page.rate, Wallet.currency)
            }

            Repeater {
                model: page.methods

                delegate: BackgroundItem {
                    width: column.width
                    height: methodBody.height + Theme.paddingLarge
                    enabled: page.methodOk(modelData.rail)
                    opacity: enabled ? 1.0 : Theme.opacityLow
                    highlighted: page.activeMethod() === modelData.rail
                    onClicked: page.chosen = modelData.rail

                    Row {
                        id: methodBody
                        x: Theme.horizontalPageMargin
                        y: Theme.paddingMedium
                        width: parent.width - 2 * x
                        height: methodText.height
                        spacing: Theme.paddingMedium

                        RailIcon {
                            width: Theme.iconSizeSmall
                            height: width
                            anchors.verticalCenter: parent.verticalCenter
                            rail: modelData.rail
                            color: parent.parent.enabled ? Theme.primaryColor : Theme.secondaryColor
                        }
                        Column {
                            id: methodText
                            width: parent.width - Theme.iconSizeSmall - parent.spacing
                            spacing: Theme.paddingSmall

                            Label {
                                width: parent.width
                                text: modelData.title
                            }
                            Label {
                                width: parent.width
                                visible: page.methodOk(modelData.rail)
                                color: Theme.secondaryColor
                                font.pixelSize: Theme.fontSizeSmall
                                wrapMode: Text.Wrap
                                maximumLineCount: modelData.rail === "lightning" ? 2 : 3
                                elide: Text.ElideMiddle
                                text: page.methodAddress(modelData.rail)
                            }
                            Label {
                                width: parent.width
                                visible: text.length > 0
                                color: Theme.secondaryColor
                                font.pixelSize: Theme.fontSizeSmall
                                wrapMode: Text.Wrap
                                text: page.extraDetail(modelData.rail)
                            }
                        }
                    }
                }
            }

            Label {
                x: Theme.horizontalPageMargin
                width: parent.width - 2 * x
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
                color: Theme.highlightColor
                visible: page.failure.length > 0
                text: page.failure
            }
        }
        VerticalScrollDecorator {}
    }

    Button {
        id: payButton
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Theme.paddingLarge
        preferredWidth: Theme.buttonWidthLarge
        enabled: page.activeMethod().length > 0 && !page.waiting && !Wallet.busy
        text: page.waiting ? "Paying…" : (page.activeMethod().length ? "Pay with " + page.methodTitle(page.activeMethod()) : "Pay")
        onClicked: page.askConfirm()
    }
}
