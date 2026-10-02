TARGET = barkauf

CONFIG += sailfishapp c++11

QT += core qml quick dbus multimedia

include(vendor/barcode/barcode.pri)

SOURCES += \
    src/main.cpp \
    src/walletcontroller.cpp \
    src/nfccontroller.cpp

HEADERS += \
    src/walletcontroller.h \
    src/nfccontroller.h

OTHER_FILES += \
    qml/*.qml \
    qml/*.js \
    qml/pages/*.qml \
    barkauf.desktop \
    rpm/barkauf.spec

desktop.files = barkauf.desktop

SAILFISHAPP_ICONS = 86x86 108x108 128x128 172x172 256x256
