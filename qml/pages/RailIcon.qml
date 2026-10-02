import QtQuick 2.0

// Lucide-style marks used by Noah: ship (Ark), zap (Lightning), link (on-chain).
Canvas {
    id: icon
    property string rail: "onchain"
    property color color: "white"

    // A list delegate's canvas often paints once at zero size, then never
    // again until the page is left and opened. Repaint whenever it is shown.
    renderTarget: Canvas.Image
    onAvailableChanged: if (available) requestPaint()
    onVisibleChanged: if (visible) requestPaint()
    Component.onCompleted: paintNow.start()

    Timer {
        id: paintNow
        interval: 0
        onTriggered: icon.requestPaint()
    }

    onPaint: {
        var ctx = getContext("2d")
        ctx.clearRect(0, 0, width, height)
        ctx.save()
        ctx.scale(width / 24, height / 24)
        ctx.strokeStyle = color
        ctx.fillStyle = color
        ctx.lineWidth = 1.8
        ctx.lineCap = "round"
        ctx.lineJoin = "round"
        if (rail === "lightning")
            paintZap(ctx)
        else if (rail === "ark")
            paintShip(ctx)
        else if (rail === "board")
            paintBoard(ctx)
        else if (rail === "offboard")
            paintOffboard(ctx)
        else if (rail === "request")
            paintRequest(ctx)
        else
            paintLink(ctx)
        ctx.restore()
    }
    onRailChanged: requestPaint()
    onColorChanged: requestPaint()
    onWidthChanged: requestPaint()
    onHeightChanged: requestPaint()

    function paintZap(ctx) {
        ctx.beginPath()
        ctx.moveTo(13, 2)
        ctx.lineTo(3, 14)
        ctx.lineTo(12, 14)
        ctx.lineTo(11, 22)
        ctx.lineTo(21, 10)
        ctx.lineTo(12, 10)
        ctx.closePath()
        ctx.stroke()
    }

    function paintLink(ctx) {
        ctx.beginPath()
        ctx.arc(14.6, 8.4, 4.3, 0.4, 2.5, false)
        ctx.stroke()
        ctx.beginPath()
        ctx.arc(9.4, 15.6, 4.3, 3.5, 5.6, false)
        ctx.stroke()
    }

    function paintBoard(ctx) {
        ctx.beginPath()
        ctx.moveTo(15, 3)
        ctx.lineTo(15, 15)
        ctx.moveTo(10, 10)
        ctx.lineTo(15, 15)
        ctx.lineTo(20, 10)
        ctx.moveTo(4, 21)
        ctx.lineTo(4, 14)
        ctx.lineTo(10, 14)
        ctx.stroke()
    }

    function paintOffboard(ctx) {
        ctx.beginPath()
        ctx.moveTo(15, 15)
        ctx.lineTo(15, 3)
        ctx.moveTo(10, 8)
        ctx.lineTo(15, 3)
        ctx.lineTo(20, 8)
        ctx.moveTo(4, 21)
        ctx.lineTo(4, 14)
        ctx.lineTo(10, 14)
        ctx.stroke()
    }

    function paintRequest(ctx) {
        ctx.lineWidth = 1.6
        ctx.beginPath()
        ctx.moveTo(5, 3)
        ctx.lineTo(19, 3)
        ctx.quadraticCurveTo(21, 3, 21, 5)
        ctx.lineTo(21, 19)
        ctx.quadraticCurveTo(21, 21, 19, 21)
        ctx.lineTo(5, 21)
        ctx.quadraticCurveTo(3, 21, 3, 19)
        ctx.lineTo(3, 5)
        ctx.quadraticCurveTo(3, 3, 5, 3)
        ctx.stroke()
        ctx.fillRect(5.4, 5.4, 4.4, 4.4)
        ctx.fillRect(14.2, 5.4, 4.4, 4.4)
        ctx.fillRect(5.4, 14.2, 4.4, 4.4)
        ctx.fillRect(14.2, 14.2, 1.7, 1.7)
        ctx.fillRect(17, 14.2, 1.7, 1.7)
        ctx.fillRect(14.2, 17, 1.7, 1.7)
        ctx.fillRect(17, 17, 1.7, 1.7)
    }

    function paintShip(ctx) {
        ctx.beginPath()
        ctx.moveTo(5, 13)
        ctx.lineTo(5, 7)
        ctx.quadraticCurveTo(5, 5, 7, 5)
        ctx.lineTo(17, 5)
        ctx.quadraticCurveTo(19, 5, 19, 7)
        ctx.lineTo(19, 13)
        ctx.moveTo(12, 2)
        ctx.lineTo(12, 5)
        ctx.moveTo(12, 10.2)
        ctx.lineTo(12, 14)
        ctx.moveTo(21, 14)
        ctx.lineTo(12.8, 10.5)
        ctx.quadraticCurveTo(12, 10.1, 11.2, 10.5)
        ctx.lineTo(3, 14)
        ctx.moveTo(3, 16)
        ctx.quadraticCurveTo(6, 20, 9.5, 17.5)
        ctx.quadraticCurveTo(12, 16, 14.5, 18.5)
        ctx.quadraticCurveTo(18, 21, 21, 17)
        ctx.stroke()
    }
}
