import QtQuick 2.0
import Sailfish.Silica 1.0
import QtMultimedia 5.4
import Barkauf 1.0

Page {
    id: scanPage
    signal accepted(string text)
    property bool done: false
    property bool cameraChosen: false

    readonly property bool cameraWanted: status === PageStatus.Active
            && !done
            && Qt.application.state === Qt.ApplicationActive

    function orientationAngle() {
        switch (orientation) {
        case Orientation.Landscape: return 90
        case Orientation.PortraitInverted: return 180
        case Orientation.LandscapeInverted: return 270
        default: return 0
        }
    }

    function finish(text) {
        if (done || text.length === 0)
            return
        done = true
        scanner.stopScanning()
        accepted(text)
        pageStack.pop()
    }

    function chooseBackCamera() {
        if (cameraChosen)
            return false
        cameraChosen = true
        var backId = ""
        var fallback = ""
        var cams = QtMultimedia.availableCameras
        for (var i = 0; i < cams.length; ++i) {
            var device = cams[i]
            if (!fallback)
                fallback = device.deviceId
            if (device.position === Camera.BackFace && !backId)
                backId = device.deviceId
        }
        var id = backId || fallback
        if (id && camera.deviceId !== id) {
            camera.deviceId = id
            reloadTimer.restart()
            return true
        }
        return false
    }

    function updateGrabRect() {
        var p = viewFinder.mapToItem(null, 0, 0)
        grabber.viewFinderRect = Qt.rect(p.x, p.y, viewFinder.width, viewFinder.height)
    }

    onCameraWantedChanged: {
        if (!cameraWanted)
            scanner.stopScanning()
    }
    onOrientationChanged: {
        scanner.rotation = orientationAngle()
        if (camera.cameraState !== Camera.UnloadedState)
            reloadTimer.restart()
    }
    Component.onDestruction: scanner.stopScanning()

    VideoOutput {
        id: viewFinder
        anchors.fill: parent
        fillMode: VideoOutput.PreserveAspectFit

        property bool ready: false

        onXChanged: scanPage.updateGrabRect()
        onYChanged: scanPage.updateGrabRect()
        onWidthChanged: scanPage.updateGrabRect()
        onHeightChanged: scanPage.updateGrabRect()
        Component.onCompleted: {
            ready = true
            scanPage.updateGrabRect()
        }

        source: Camera {
            id: camera
            flash.mode: Camera.FlashOff
            captureMode: Camera.CaptureStillImage
            videoRecorder.frameRate: 30
            imageProcessing.whiteBalanceMode: CameraImageProcessing.WhiteBalanceTungsten
            cameraState: (viewFinder.ready && scanPage.cameraWanted && !reloadTimer.running)
                         ? Camera.ActiveState : Camera.UnloadedState
            exposure {
                exposureCompensation: 1.0
                exposureMode: Camera.ExposureAuto
            }
            focus {
                focusMode: Camera.FocusContinuous
                focusPointMode: Camera.FocusPointAuto
            }
            onCameraStatusChanged: {
                if (cameraStatus !== Camera.ActiveStatus)
                    return
                if (scanPage.chooseBackCamera())
                    return
                if (scanPage.cameraWanted)
                    scanner.startScanning(0)
            }
            onError: {
                console.log("barkauf camera:", errorString)
                reloadTimer.restart()
            }
        }
    }

    Timer {
        id: reloadTimer
        interval: 100
    }

    BarcodeScanner {
        id: scanner
        rotation: scanPage.orientationAngle()
        onNeedImage: grabber.requestImage()
        onDecodingFinished: {
            if (result.ok)
                scanPage.finish(result.text)
        }
    }

    BarcodeImageGrabber {
        id: grabber
        canGrab: scanPage.cameraWanted
        viewFinderItem: viewFinder
        onImageGrabbed: scanner.scanImage(image, viewPort)
    }

    PageHeader {
        title: "Scan"
    }

    Label {
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        anchors.margins: Theme.paddingLarge
        wrapMode: Text.Wrap
        color: Theme.highlightColor
        text: "Point the camera at a payment QR"
    }
}
