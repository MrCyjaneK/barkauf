#include "nfccontroller.h"

#include <QCoreApplication>
#include <QDBusAbstractAdaptor>
#include <QDBusArgument>
#include <QDBusConnection>
#include <QDBusInterface>
#include <QDBusObjectPath>
#include <QDBusReply>
#include <QStringList>

namespace {

const char kService[] = "org.sailfishos.nfc.daemon";
const char kDaemonIface[] = "org.sailfishos.nfc.Daemon";
const char kAdapterIface[] = "org.sailfishos.nfc.Adapter";
const char kTagIface[] = "org.sailfishos.nfc.Tag";
const char kNdefIface[] = "org.sailfishos.nfc.NDEF";
const char kSharePath[] = "/org/barkauf/NdefShare";
const unsigned kReader = 0x02;
const unsigned kEmulate = 0x08;
const unsigned kAllowImplicit = 0x01;

const char kCcFid[] = "\xe1\x03";
const char kNdefFid[] = "\xe1\x04";

QStringList objectPaths(const QVariant &value)
{
    QStringList out;
    if (!value.canConvert<QDBusArgument>())
        return out;
    const QDBusArgument arg = value.value<QDBusArgument>();
    if (arg.currentType() != QDBusArgument::ArrayType)
        return out;
    arg.beginArray();
    while (!arg.atEnd()) {
        QDBusObjectPath path;
        arg >> path;
        out << path.path();
    }
    arg.endArray();
    return out;
}

QByteArray byteArg(const QVariant &value)
{
    if (value.canConvert<QByteArray>())
        return value.toByteArray();
    return QByteArray();
}

QString ndefText(const QByteArray &payload)
{
    if (payload.isEmpty())
        return QString();
    const unsigned char status = static_cast<unsigned char>(payload.at(0));
    const int langLen = status & 0x3f;
    if (payload.size() < 1 + langLen)
        return QString();
    const QByteArray text = payload.mid(1 + langLen);
    if (status & 0x80)
        return QString::fromUtf16(reinterpret_cast<const ushort *>(text.constData()), text.size() / 2);
    return QString::fromUtf8(text);
}

QByteArray textRecord(const QString &text)
{
    const QByteArray body = text.toUtf8();
    QByteArray payload;
    payload.append(char(2));
    payload.append("en");
    payload.append(body);

    QByteArray rec;
    const bool shortRec = payload.size() <= 255;
    rec.append(char(shortRec ? 0xd1 : 0xc1));
    rec.append(char(1));
    if (shortRec) {
        rec.append(char(payload.size()));
    } else {
        const quint32 n = quint32(payload.size());
        rec.append(char((n >> 24) & 0xff));
        rec.append(char((n >> 16) & 0xff));
        rec.append(char((n >> 8) & 0xff));
        rec.append(char(n & 0xff));
    }
    rec.append('T');
    rec.append(payload);
    return rec;
}

class ShareAdaptor : public QDBusAbstractAdaptor
{
    Q_OBJECT
    Q_CLASSINFO("D-Bus Interface", "org.sailfishos.nfc.LocalHostApp")
public:
    explicit ShareAdaptor(NfcController *nfc)
        : QDBusAbstractAdaptor(nfc)
        , m_nfc(nfc)
    {
    }

public slots:
    int GetInterfaceVersion() { return 1; }

    void Start(const QDBusObjectPath &) { m_nfc->resetSelection(); }
    void Restart(const QDBusObjectPath &) { m_nfc->resetSelection(); }
    void Stop(const QDBusObjectPath &) {}
    void ImplicitSelect(const QDBusObjectPath &) {}
    void Select(const QDBusObjectPath &) {}
    void Deselect(const QDBusObjectPath &) { m_nfc->resetSelection(); }

    void Process(const QDBusObjectPath &,
                 uchar cla, uchar ins, uchar p1, uchar p2,
                 const QByteArray &data, uint le,
                 const QDBusMessage &message)
    {
        unsigned char sw1 = 0x6f;
        unsigned char sw2 = 0x00;
        unsigned responseId = 0;
        QByteArray response;
        if (cla == 0x00 && ins == 0xa4)
            m_nfc->selectFile(data, &sw1, &sw2);
        else if (cla == 0x00 && ins == 0xb0 && !(p1 & 0x80))
            response = m_nfc->readBinary((int(p1) << 8) | p2, int(le), &sw1, &sw2, &responseId);
        QDBusMessage reply = message.createReply();
        reply << response << sw1 << sw2 << responseId;
        QDBusConnection::systemBus().send(reply);
    }

    void ResponseStatus(uint, bool) {}

private:
    NfcController *m_nfc;
};

} // namespace

NfcController::NfcController(QObject *parent)
    : QObject(parent)
    , m_started(false)
    , m_enabled(false)
    , m_powered(false)
    , m_sharing(false)
    , m_readerMode(0)
    , m_shareMode(0)
    , m_selected(0)
    , m_daemon(0)
{
    new ShareAdaptor(this);
    QDBusConnection::systemBus().registerObject(QString::fromLatin1(kSharePath), this,
                                                QDBusConnection::ExportAdaptors);
    connect(QCoreApplication::instance(), &QCoreApplication::aboutToQuit, this, &NfcController::stop);
}

NfcController::~NfcController()
{
    stop();
    QDBusConnection::systemBus().unregisterObject(QString::fromLatin1(kSharePath));
}

void NfcController::setShareText(const QString &text)
{
    if (m_shareText == text)
        return;
    m_shareText = text;
    rebuildNdef();
    emit changed();
}

void NfcController::setStatus(const QString &text)
{
    if (m_status == text)
        return;
    m_status = text;
    emit changed();
}

void NfcController::rebuildNdef()
{
    const QByteArray message = textRecord(m_shareText);
    m_ndef.clear();
    m_ndef.append(char((message.size() >> 8) & 0xff));
    m_ndef.append(char(message.size() & 0xff));
    m_ndef.append(message);

    unsigned size = unsigned(m_ndef.size());
    if (size > 0x7fff)
        size = 0x7fff;
    m_cc = QByteArray::fromHex("000f20ffff");
    m_cc.append(char(0xff));
    m_cc.append(char(0xff));
    m_cc.append(QByteArray::fromHex("0406e104"));
    m_cc.append(char((size >> 8) & 0x7f));
    m_cc.append(char(size & 0xff));
    m_cc.append(QByteArray::fromHex("00ff"));
}

void NfcController::resetSelection()
{
    m_selected = 0;
}

void NfcController::selectFile(const QByteArray &fid, unsigned char *sw1, unsigned char *sw2)
{
    if (fid == QByteArray(kCcFid, 2))
        m_selected = 1;
    else if (fid == QByteArray(kNdefFid, 2))
        m_selected = 2;
    else {
        m_selected = 0;
        *sw1 = 0x6a;
        *sw2 = 0x82;
        return;
    }
    *sw1 = 0x90;
    *sw2 = 0x00;
}

QByteArray NfcController::readBinary(int offset, int le, unsigned char *sw1, unsigned char *sw2, unsigned *responseId)
{
    const QByteArray *file = 0;
    if (m_selected == 1)
        file = &m_cc;
    else if (m_selected == 2)
        file = &m_ndef;
    if (!file) {
        *sw1 = 0x6f;
        *sw2 = 0x00;
        return QByteArray();
    }
    if (offset < 0 || offset >= file->size()) {
        *sw1 = 0x90;
        *sw2 = 0x00;
        return QByteArray();
    }
    int count = file->size() - offset;
    if (le > 0 && count > le)
        count = le;
    *sw1 = 0x90;
    *sw2 = 0x00;
    *responseId = m_selected == 2 ? 1u : 0u;
    return file->mid(offset, count);
}

void NfcController::start()
{
    if (m_started)
        return;
    QDBusConnection bus = QDBusConnection::systemBus();
    if (!bus.isConnected()) {
        setStatus(QStringLiteral("system bus is not available"));
        return;
    }
    m_daemon = new QDBusInterface(QString::fromLatin1(kService), QStringLiteral("/"),
                                  QString::fromLatin1(kDaemonIface), bus, this);
    const QDBusMessage reply = m_daemon->call(QStringLiteral("GetAdapters"));
    if (reply.type() == QDBusMessage::ErrorMessage) {
        setStatus(reply.errorMessage());
        return;
    }
    const QStringList adapters = objectPaths(reply.arguments().value(0));
    if (adapters.isEmpty()) {
        setStatus(QStringLiteral("no NFC adapter"));
        return;
    }
    m_adapter = adapters.first();
    bus.connect(QString::fromLatin1(kService), m_adapter, QString::fromLatin1(kAdapterIface),
                QStringLiteral("TagsChanged"), this, SLOT(onTagsChanged(QDBusMessage)));
    refreshAdapter();
    requestMode(kReader, &m_readerMode);
    m_started = true;
    if (!m_enabled)
        setStatus(QStringLiteral("Turn NFC on in Settings"));
    else
        setStatus(QStringLiteral("waiting for a tag"));
}

void NfcController::stop()
{
    if (!m_started)
        return;
    setSharing(false);
    releaseMode(&m_readerMode);
    if (!m_adapter.isEmpty()) {
        QDBusConnection::systemBus().disconnect(QString::fromLatin1(kService), m_adapter,
                                                QString::fromLatin1(kAdapterIface),
                                                QStringLiteral("TagsChanged"),
                                                this, SLOT(onTagsChanged(QDBusMessage)));
    }
    m_started = false;
}

void NfcController::refreshAdapter()
{
    if (m_adapter.isEmpty())
        return;
    QDBusInterface adapter(QString::fromLatin1(kService), m_adapter, QString::fromLatin1(kAdapterIface),
                           QDBusConnection::systemBus());
    const QDBusMessage enabled = adapter.call(QStringLiteral("GetEnabled"));
    const QDBusMessage powered = adapter.call(QStringLiteral("GetPowered"));
    if (enabled.type() != QDBusMessage::ErrorMessage)
        m_enabled = enabled.arguments().value(0).toBool();
    if (powered.type() != QDBusMessage::ErrorMessage)
        m_powered = powered.arguments().value(0).toBool();
    emit changed();
}

void NfcController::requestMode(unsigned enable, unsigned *id)
{
    if (!m_daemon || *id != 0)
        return;
    const QDBusMessage reply = m_daemon->call(QStringLiteral("RequestMode"), enable, unsigned(0));
    if (reply.type() == QDBusMessage::ErrorMessage) {
        setStatus(reply.errorMessage());
        return;
    }
    *id = reply.arguments().value(0).toUInt();
}

void NfcController::releaseMode(unsigned *id)
{
    if (!m_daemon || *id == 0)
        return;
    m_daemon->call(QStringLiteral("ReleaseMode"), *id);
    *id = 0;
}

void NfcController::setSharing(bool on)
{
    if (on == m_sharing)
        return;
    if (!m_started)
        start();
    if (!m_daemon) {
        setStatus(QStringLiteral("NFC daemon is not available"));
        return;
    }
    if (on) {
        if (m_shareText.trimmed().isEmpty()) {
            setStatus(QStringLiteral("no payment URI to share"));
            return;
        }
        rebuildNdef();
        const QByteArray aid = QByteArray::fromHex("d2760000850101");
        const QDBusMessage reply = m_daemon->call(QStringLiteral("RegisterLocalHostApp"),
                                                   QVariant::fromValue(QDBusObjectPath(QString::fromLatin1(kSharePath))),
                                                   QVariant(QStringLiteral("barkauf")),
                                                   QVariant(aid),
                                                   QVariant(uint(kAllowImplicit)));
        if (reply.type() == QDBusMessage::ErrorMessage) {
            setStatus(reply.errorMessage());
            return;
        }
        requestMode(kEmulate, &m_shareMode);
        m_sharing = true;
        setStatus(QStringLiteral("sharing payment URI"));
    } else {
        m_daemon->call(QStringLiteral("UnregisterLocalHostApp"),
                       QVariant::fromValue(QDBusObjectPath(QString::fromLatin1(kSharePath))));
        releaseMode(&m_shareMode);
        m_sharing = false;
        setStatus(m_enabled ? QStringLiteral("waiting for a tag")
                            : QStringLiteral("Turn NFC on in Settings"));
    }
    emit changed();
}

void NfcController::onTagsChanged(const QDBusMessage &message)
{
    refreshAdapter();
    const QStringList tags = objectPaths(message.arguments().value(0));
    if (tags.isEmpty()) {
        m_lastPaid.clear();
        if (m_started && !m_sharing)
            setStatus(m_enabled ? QStringLiteral("waiting for a tag")
                                : QStringLiteral("Turn NFC on in Settings"));
        return;
    }
    readTag(tags.first());
}

void NfcController::readTag(const QString &path)
{
    QDBusInterface tag(QString::fromLatin1(kService), path, QString::fromLatin1(kTagIface),
                       QDBusConnection::systemBus());
    const QDBusMessage records = tag.call(QStringLiteral("GetNdefRecords"));
    if (records.type() == QDBusMessage::ErrorMessage) {
        setStatus(records.errorMessage());
        return;
    }
    const QStringList ndefs = objectPaths(records.arguments().value(0));
    QString text;
    for (int i = 0; i < ndefs.size(); ++i) {
        QDBusInterface ndef(QString::fromLatin1(kService), ndefs.at(i), QString::fromLatin1(kNdefIface),
                            QDBusConnection::systemBus());
        const QDBusMessage all = ndef.call(QStringLiteral("GetAll"));
        if (all.type() == QDBusMessage::ErrorMessage || all.arguments().size() < 7)
            continue;
        const QByteArray type = byteArg(all.arguments().at(4));
        if (type != QByteArray("T"))
            continue;
        const QString candidate = ndefText(byteArg(all.arguments().at(6))).trimmed();
        if (candidate.isEmpty())
            continue;
        text = candidate;
        if (candidate.contains(QStringLiteral("bitcoin:"), Qt::CaseInsensitive))
            break;
    }
    if (text.isEmpty()) {
        setStatus(QStringLiteral("tag has no NDEF text"));
        return;
    }
    m_lastRead = text;
    emit changed();
    emit textRead(text);
    if (text == m_lastPaid)
        return;
    m_lastPaid = text;
    setStatus(QStringLiteral("read a payment URI"));
}

#include "nfccontroller.moc"
