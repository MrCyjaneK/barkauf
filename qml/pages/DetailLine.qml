import QtQuick 2.0
import Sailfish.Silica 1.0

Item {
    id: line
    property string label: ""
    property string value: ""
    property bool copyable: false
    property bool link: false
    property bool justCopied: false

    width: parent ? parent.width : 0
    height: visible ? Math.max(Theme.itemSizeSmall, valueLabel.implicitHeight + Theme.paddingMedium) : 0
    visible: value.length > 0

    Timer {
        id: copiedTimer
        interval: 1200
        onTriggered: line.justCopied = false
    }

    Label {
        x: Theme.horizontalPageMargin
        width: parent.width * 0.34
        anchors.verticalCenter: parent.verticalCenter
        color: Theme.secondaryColor
        font.pixelSize: Theme.fontSizeSmall
        text: line.label
        elide: Text.ElideRight
    }
    Label {
        id: valueLabel
        anchors.left: parent.left
        anchors.leftMargin: Theme.horizontalPageMargin + parent.width * 0.34 + Theme.paddingSmall
        anchors.right: parent.right
        anchors.rightMargin: Theme.horizontalPageMargin
        anchors.verticalCenter: parent.verticalCenter
        horizontalAlignment: Text.AlignRight
        wrapMode: line.link ? Text.WrapAnywhere : Text.Wrap
        elide: line.link ? Text.ElideNone : Text.ElideMiddle
        maximumLineCount: line.link ? 8 : 3
        font.pixelSize: Theme.fontSizeSmall
        color: line.justCopied || line.link ? Theme.highlightColor : Theme.primaryColor
        textFormat: line.link ? Text.PlainText : Text.RichText
        text: line.justCopied ? "Copied" : line.value
    }
    MouseArea {
        anchors.fill: parent
        enabled: line.copyable || line.link
        onClicked: {
            if (line.link) {
                Qt.openUrlExternally(line.value)
                return
            }
            Clipboard.text = line.value
            line.justCopied = true
            copiedTimer.restart()
        }
    }
}