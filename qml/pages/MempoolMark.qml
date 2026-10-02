import QtQuick 2.0
import Sailfish.Silica 1.0

Canvas {
    id: mark

    implicitWidth: Theme.iconSizeMedium
    implicitHeight: implicitWidth
    antialiasing: true
    renderTarget: Canvas.Image

    onAvailableChanged: if (available) requestPaint()
    onVisibleChanged: if (visible) requestPaint()
    onWidthChanged: requestPaint()
    onHeightChanged: requestPaint()
    Component.onCompleted: paintNow.start()

    Timer {
        id: paintNow
        interval: 0
        onTriggered: mark.requestPaint()
    }

    onPaint: {
        var ctx = getContext("2d")
        ctx.clearRect(0, 0, width, height)
        if (width < 2 || height < 2)
            return
        var s = Math.min(width, height) / 24
        ctx.save()
        ctx.scale(s, s)
        var blocks = [
            ["#7c5cff", 1, 1],
            ["#3dd6c6", 13, 1],
            ["#b79bff", 1, 13],
            ["#5b8cff", 13, 13]
        ]
        for (var i = 0; i < blocks.length; ++i) {
            ctx.fillStyle = blocks[i][0]
            ctx.fillRect(blocks[i][1], blocks[i][2], 10, 10)
        }
        ctx.restore()
    }
}
