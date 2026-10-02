import QtQuick 2.0
import Sailfish.Silica 1.0

Dialog {
    property string amountText: ""
    property string methodName: ""

    canAccept: true
    backNavigation: false

    SilicaFlickable {
        anchors.fill: parent
        contentHeight: column.height + Theme.paddingLarge

        Column {
            id: column
            width: parent.width
            spacing: Theme.paddingLarge

            DialogHeader {
                acceptText: "Done"
                cancelText: ""
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeHuge
                font.bold: true
                text: "Sent"
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pixelSize: Theme.fontSizeExtraLarge
                textFormat: Text.RichText
                text: amountText
            }
            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                color: Theme.secondaryColor
                text: methodName
            }
        }
        VerticalScrollDecorator {}
    }
}
