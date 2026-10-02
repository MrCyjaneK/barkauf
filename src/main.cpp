#include <sailfishapp.h>

#include <QGuiApplication>
#include <QQuickView>
#include <QQmlContext>

#include "walletcontroller.h"
#include "nfccontroller.h"
#include "BarcodeScanner.h"
#include "BarcodeImageGrabber.h"

Q_DECL_EXPORT int main(int argc, char *argv[])
{
    QScopedPointer<QGuiApplication> app(SailfishApp::application(argc, argv));
    app->setOrganizationName(QStringLiteral("barkauf"));
    app->setApplicationName(QStringLiteral("barkauf"));

    QScopedPointer<QQuickView> view(SailfishApp::createView());

    WalletController wallet;
    NfcController nfc;
    qmlRegisterType<BarcodeScanner>("Barkauf", 1, 0, "BarcodeScanner");
    qmlRegisterType<BarcodeImageGrabber>("Barkauf", 1, 0, "BarcodeImageGrabber");
    view->rootContext()->setContextProperty(QStringLiteral("Wallet"), &wallet);
    view->rootContext()->setContextProperty(QStringLiteral("Nfc"), &nfc);

    view->setSource(SailfishApp::pathToMainQml());
    view->show();
    return app->exec();
}
