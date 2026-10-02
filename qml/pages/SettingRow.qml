import QtQuick 2.0
import Sailfish.Silica 1.0

BackgroundItem {
    id: row
    property string title: ""
    property string subtitle: ""
    property string description: ""

    width: parent ? parent.width : 0
    height: content.implicitHeight + Theme.paddingLarge

    Column {
        id: content
        x: Theme.horizontalPageMargin
        width: parent.width - 2 * x - Theme.iconSizeSmall
        anchors.verticalCenter: parent.verticalCenter
        spacing: Theme.paddingSmall / 2

        Label {
            width: parent.width
            text: row.title
            truncationMode: TruncationMode.Fade
        }
        Label {
            width: parent.width
            visible: row.subtitle.length > 0
            color: Theme.secondaryColor
            font.pixelSize: Theme.fontSizeSmall
            text: row.subtitle
            truncationMode: TruncationMode.Fade
        }
        Label {
            width: parent.width
            visible: row.description.length > 0
            wrapMode: Text.Wrap
            color: Theme.secondaryColor
            font.pixelSize: Theme.fontSizeSmall
            text: row.description
        }
    }

    Image {
        anchors.right: parent.right
        anchors.rightMargin: Theme.horizontalPageMargin
        anchors.verticalCenter: parent.verticalCenter
        source: "image://theme/icon-m-right"
    }
}
