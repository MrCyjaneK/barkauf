#include "walletcontroller.h"

#include <QDBusConnection>
#include <QDBusInterface>
#include <QDBusPendingCall>
#include <QCoreApplication>
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QSettings>
#include <QStandardPaths>
#include <QUrl>
#include <QVariant>

WalletController::WalletController(QObject *parent)
    : QObject(parent)
    , m_waiting(false)
    , m_busy(false)
    , m_ready(false)
    , m_hasWallet(false)
    , m_paying(false)
    , m_fromCache(false)
    , m_canArk(false)
    , m_canLightning(false)
    , m_canOnchain(false)
    , m_canAll(false)
    , m_spendable(0)
    , m_total(0)
    , m_onchainTotal(0)
    , m_onchainConfirmed(0)
    , m_onchainPending(0)
    , m_offchainTotal(0)
    , m_pendingSend(0)
    , m_pendingInRound(0)
    , m_pendingExit(0)
    , m_pendingBoard(0)
    , m_claimableReceive(0)
    , m_historyReady(false)
    , m_announce(false)
    , m_stopped(false)
    , m_amountSat(0)
    , m_btcUsd(0)
    , m_currency(QStringLiteral("USD"))
    , m_rateCurrency(QStringLiteral("USD"))
    , m_vtxoSerial(0)
    , m_tipHeight(0)
    , m_vtxoExitDelta(0)
    , m_minBoard(0)
    , m_hasBoardFee(false)
    , m_boardGross(0)
    , m_boardFee(0)
    , m_boardNet(0)
    , m_boardAmount(0)
    , m_hasPayFee(false)
    , m_payFeeMethod()
    , m_payGross(0)
    , m_payFee(0)
    , m_payNet(0)
    , m_lnurlReady(false)
    , m_lnurlFixed(false)
    , m_lnurlAmount(0)
    , m_refreshCount(0)
    , m_refreshSats(0)
    , m_refreshHeight(0)
    , m_hasRefreshFee(false)
    , m_refreshGross(0)
    , m_refreshFee(0)
    , m_refreshNet(0)
    , m_refreshScheduled(false)
    , m_refreshDetail()
    , m_refreshInRound(0)
{
    m_datadir = QStandardPaths::writableLocation(QStandardPaths::AppDataLocation);
    QDir().mkpath(m_datadir);
    QSettings settings;
    m_currency = settings.value(QStringLiteral("currency"), QStringLiteral("USD")).toString();
    m_rateCurrency = settings.value(QStringLiteral("rateCurrency"), QStringLiteral("USD")).toString();
    m_displayName = settings.value(QStringLiteral("displayName")).toString();
    if (m_currency.isEmpty())
        m_currency = QStringLiteral("USD");
    loadCache();
    connect(&m_proc, SIGNAL(readyReadStandardOutput()), this, SLOT(onReadyRead()));
    connect(&m_proc, SIGNAL(readyReadStandardError()), this, SLOT(onReadyReadStderr()));
    connect(&m_proc, SIGNAL(finished(int,QProcess::ExitStatus)), this, SLOT(onFinished(int,QProcess::ExitStatus)));
    m_movementTimer.setSingleShot(true);
    connect(&m_movementTimer, SIGNAL(timeout()), this, SLOT(syncFromWatch()));
    connect(QCoreApplication::instance(), SIGNAL(aboutToQuit()), this, SLOT(shutdown()));
    setActivity(QStringLiteral("Starting wallet"));
    refresh();
}

WalletController::~WalletController()
{
    shutdown();
}

void WalletController::shutdown()
{
    if (m_stopped)
        return;
    m_stopped = true;
    m_movementTimer.stop();
    disconnect(&m_proc, 0, this, 0);
    m_queue.clear();
    m_waiting = false;
    if (m_proc.state() == QProcess::NotRunning)
        return;
    m_proc.closeWriteChannel();
    if (m_proc.waitForFinished(800))
        return;
    m_proc.terminate();
    if (m_proc.waitForFinished(400))
        return;
    m_proc.kill();
    m_proc.waitForFinished(400);
}

QString WalletController::binaryPath() const
{
    const QByteArray env = qgetenv("BARKAUF_WALLET");
    if (!env.isEmpty())
        return QString::fromLocal8Bit(env);
    const QString installed = QStringLiteral("/usr/libexec/barkauf/barkauf-wallet");
    if (QFileInfo(installed).isExecutable())
        return installed;
    return QCoreApplication::applicationDirPath() + QStringLiteral("/../libexec/barkauf/barkauf-wallet");
}

void WalletController::setActivity(const QString &text)
{
    if (m_activity == text)
        return;
    m_activity = text;
    emit changed();
}

static QString groupedSats(qint64 amount)
{
    QString digits = QString::number(amount < 0 ? -amount : amount);
    for (int i = digits.size() - 3; i > 0; i -= 3)
        digits.insert(i, QChar(0x2009));
    return digits + QStringLiteral(" sats");
}

void WalletController::publishNotification(const QString &summary, const QString &body)
{
    QDBusInterface bus(QStringLiteral("org.freedesktop.Notifications"),
                       QStringLiteral("/org/freedesktop/Notifications"),
                       QStringLiteral("org.freedesktop.Notifications"),
                       QDBusConnection::sessionBus());
    if (!bus.isValid())
        return;
    bus.asyncCall(QStringLiteral("Notify"),
                  QStringLiteral("Barkauf"),
                  uint(0),
                  QString(),
                  summary,
                  body,
                  QStringList(),
                  QVariantMap(),
                  int(-1));
}

void WalletController::rememberHistory()
{
    const QString id = m_history.isEmpty()
            ? QString()
            : m_history.first().toMap().value(QStringLiteral("id")).toString();
    if (!m_historyReady) {
        m_historyReady = true;
        m_lastHistoryId = id;
        m_announce = false;
        return;
    }
    const bool announce = m_announce;
    m_announce = false;
    if (id.isEmpty() || id == m_lastHistoryId)
        return;
    const QVariantMap item = m_history.first().toMap();
    m_lastHistoryId = id;
    if (!announce)
        return;
    QString verb = QStringLiteral("Payment");
    if (item.value(QStringLiteral("transfer")).toBool())
        verb = QStringLiteral("Transfer");
    else if (item.value(QStringLiteral("direction")).toString() == QStringLiteral("outgoing"))
        verb = QStringLiteral("Sent");
    else
        verb = QStringLiteral("Received");
    const QString label = item.value(QStringLiteral("label")).toString();
    const QString summary = label.isEmpty() ? verb : verb + QStringLiteral(" · ") + label;
    publishNotification(summary, groupedSats(item.value(QStringLiteral("amount")).toLongLong()));
}

void WalletController::setBusy(bool busy)
{
    if (m_busy == busy)
        return;
    m_busy = busy;
    if (!busy)
        setActivity(QString());
    emit busyChanged();
}

void WalletController::ensureStarted()
{
    if (m_proc.state() != QProcess::NotRunning)
        return;
    m_proc.setProcessChannelMode(QProcess::SeparateChannels);
    m_proc.start(binaryPath(), QStringList() << QStringLiteral("--datadir") << m_datadir);
    if (!m_proc.waitForStarted(5000)) {
        m_error = QStringLiteral("wallet helper failed to start: ") + binaryPath();
        m_ready = true;
        m_waiting = false;
        m_queue.clear();
        setBusy(false);
        emit changed();
    }
}

void WalletController::enqueue(const QJsonObject &req)
{
    const QString op = req.value(QStringLiteral("op")).toString();
    if (op == QStringLiteral("status"))
        setActivity(QStringLiteral("Opening wallet"));
    else if (op == QStringLiteral("sync"))
        setActivity(QStringLiteral("Syncing wallet"));
    else if (op == QStringLiteral("import"))
        setActivity(QStringLiteral("Restoring wallet"));
    else if (op == QStringLiteral("generate"))
        setActivity(QStringLiteral("Creating recovery phrase"));
    else if (op == QStringLiteral("pay"))
        setActivity(QStringLiteral("Paying"));
    else if (op == QStringLiteral("uri"))
        setActivity(QStringLiteral("Updating invoice"));
    else if (op == QStringLiteral("vtxos"))
        setActivity(QStringLiteral("Loading VTXOs"));
    else if (op == QStringLiteral("ark_info"))
        setActivity(QStringLiteral("Reading Ark info"));
    else if (op == QStringLiteral("board_fee"))
        setActivity(QStringLiteral("Estimating boarding fee"));
    else if (op == QStringLiteral("pay_fee"))
        setActivity(QStringLiteral("Estimating fee"));
    else if (op == QStringLiteral("lnurl"))
        setActivity(QStringLiteral("Resolving Lightning link"));
    else if (op == QStringLiteral("board"))
        setActivity(QStringLiteral("Boarding to Ark"));
    else if (op == QStringLiteral("accel_preview"))
        setActivity(QStringLiteral("Checking mempool"));
    else if (op == QStringLiteral("accel_invoice"))
        setActivity(QStringLiteral("Creating accelerator invoice"));
    else if (op == QStringLiteral("refresh_due"))
        setActivity(QStringLiteral("Checking VTXO refresh"));
    else if (op == QStringLiteral("refresh_fee"))
        setActivity(QStringLiteral("Estimating refresh fee"));
    else if (op == QStringLiteral("refresh_vtxos"))
        setActivity(QStringLiteral("Refreshing VTXOs"));
    m_queue.enqueue(req);
    setBusy(true);
    pump();
}

void WalletController::pump()
{
    if (m_waiting || m_queue.isEmpty())
        return;
    ensureStarted();
    if (m_proc.state() != QProcess::Running)
        return;
    m_waiting = true;
    const QJsonObject req = m_queue.dequeue();
    m_currentOp = req.value(QStringLiteral("op")).toString();
    const QByteArray line = QJsonDocument(req).toJson(QJsonDocument::Compact) + '\n';
    m_proc.write(line);
}

void WalletController::refresh()
{
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("status"));
    enqueue(req);
}

void WalletController::generate()
{
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("generate"));
    enqueue(req);
}

void WalletController::importMnemonic(const QString &mnemonic)
{
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("import"));
    req.insert(QStringLiteral("mnemonic"), mnemonic.simplified());
    enqueue(req);
}

void WalletController::sync()
{
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("sync"));
    enqueue(req);
}

void WalletController::setAmount(quint64 sats)
{
    m_amountSat = sats;
    emit changed();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("uri"));
    req.insert(QStringLiteral("amount_sat"), QJsonValue::fromVariant(sats));
    enqueue(req);
}

QString WalletController::cachePath() const
{
    return m_datadir + QStringLiteral("/state-cache.json");
}

void WalletController::loadCache()
{
    QFile file(cachePath());
    if (!file.open(QIODevice::ReadOnly))
        return;
    const QJsonDocument doc = QJsonDocument::fromJson(file.readAll());
    if (!doc.isObject())
        return;
    const QJsonObject resp = doc.object();
    if (!resp.value(QStringLiteral("has_wallet")).toBool())
        return;
    m_fromCache = true;
    m_hasWallet = true;
    m_ready = true;
    m_error.clear();
    m_fingerprint = resp.value(QStringLiteral("fingerprint")).toString();
    m_ark = resp.value(QStringLiteral("ark")).toString();
    m_onchain = resp.value(QStringLiteral("onchain")).toString();
    m_bolt11 = resp.value(QStringLiteral("bolt11")).toString();
    m_bip321 = resp.value(QStringLiteral("bip321")).toString();
    m_canArk = resp.value(QStringLiteral("can_ark")).toBool();
    m_canLightning = resp.value(QStringLiteral("can_lightning")).toBool();
    m_canOnchain = resp.value(QStringLiteral("can_onchain")).toBool();
    m_canAll = resp.value(QStringLiteral("can_all")).toBool();
    m_spendable = resp.value(QStringLiteral("spendable_sats")).toVariant().toULongLong();
    m_total = resp.value(QStringLiteral("total_sats")).toVariant().toULongLong();
    m_onchainTotal = resp.value(QStringLiteral("onchain_total")).toVariant().toULongLong();
    m_onchainConfirmed = resp.value(QStringLiteral("onchain_confirmed")).toVariant().toULongLong();
    m_onchainPending = resp.value(QStringLiteral("onchain_pending")).toVariant().toULongLong();
    m_offchainTotal = resp.value(QStringLiteral("offchain_total")).toVariant().toULongLong();
    m_pendingSend = resp.value(QStringLiteral("pending_send")).toVariant().toULongLong();
    m_pendingInRound = resp.value(QStringLiteral("pending_in_round")).toVariant().toULongLong();
    m_pendingExit = resp.value(QStringLiteral("pending_exit")).toVariant().toULongLong();
    m_pendingBoard = resp.value(QStringLiteral("pending_board")).toVariant().toULongLong();
    m_claimableReceive = resp.value(QStringLiteral("claimable_receive")).toVariant().toULongLong();
    m_history = resp.value(QStringLiteral("history")).toArray().toVariantList();
    rememberHistory();
    m_btcUsd = resp.value(QStringLiteral("btc_usd")).toDouble();
    const QString png = resp.value(QStringLiteral("qr_png")).toString();
    if (!png.isEmpty()) {
        const QString path = m_datadir + QStringLiteral("/receive-qr.png");
        if (QFileInfo(path).exists()) {
            m_qrPng = png;
            m_qrImage = QUrl::fromLocalFile(path).toString();
        }
    }
}

void WalletController::saveCache()
{
    if (!m_hasWallet)
        return;
    QJsonObject obj;
    obj.insert(QStringLiteral("ok"), true);
    obj.insert(QStringLiteral("has_wallet"), true);
    obj.insert(QStringLiteral("fingerprint"), m_fingerprint);
    obj.insert(QStringLiteral("ark"), m_ark);
    obj.insert(QStringLiteral("onchain"), m_onchain);
    obj.insert(QStringLiteral("bolt11"), m_bolt11);
    obj.insert(QStringLiteral("bip321"), m_bip321);
    obj.insert(QStringLiteral("can_ark"), m_canArk);
    obj.insert(QStringLiteral("can_lightning"), m_canLightning);
    obj.insert(QStringLiteral("can_onchain"), m_canOnchain);
    obj.insert(QStringLiteral("can_all"), m_canAll);
    obj.insert(QStringLiteral("spendable_sats"), QJsonValue::fromVariant(m_spendable));
    obj.insert(QStringLiteral("total_sats"), QJsonValue::fromVariant(m_total));
    obj.insert(QStringLiteral("onchain_total"), QJsonValue::fromVariant(m_onchainTotal));
    obj.insert(QStringLiteral("onchain_confirmed"), QJsonValue::fromVariant(m_onchainConfirmed));
    obj.insert(QStringLiteral("onchain_pending"), QJsonValue::fromVariant(m_onchainPending));
    obj.insert(QStringLiteral("offchain_total"), QJsonValue::fromVariant(m_offchainTotal));
    obj.insert(QStringLiteral("pending_send"), QJsonValue::fromVariant(m_pendingSend));
    obj.insert(QStringLiteral("pending_in_round"), QJsonValue::fromVariant(m_pendingInRound));
    obj.insert(QStringLiteral("pending_exit"), QJsonValue::fromVariant(m_pendingExit));
    obj.insert(QStringLiteral("pending_board"), QJsonValue::fromVariant(m_pendingBoard));
    obj.insert(QStringLiteral("claimable_receive"), QJsonValue::fromVariant(m_claimableReceive));
    obj.insert(QStringLiteral("history"), QJsonArray::fromVariantList(m_history));
    obj.insert(QStringLiteral("btc_usd"), m_btcUsd);
    if (!m_qrPng.isEmpty())
        obj.insert(QStringLiteral("qr_png"), m_qrPng);
    QFile file(cachePath());
    if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate))
        return;
    file.write(QJsonDocument(obj).toJson(QJsonDocument::Compact));
}

void WalletController::rememberRate(double usdPerBtc)
{
    if (!(usdPerBtc > 0))
        return;
    if (usdPerBtc == m_btcUsd && m_rateCurrency == m_currency)
        return;
    m_btcUsd = usdPerBtc;
    m_rateCurrency = m_currency;
    QSettings settings;
    settings.setValue(QStringLiteral("rateCurrency"), m_rateCurrency);
    settings.setValue(QStringLiteral("btcFiat"), m_btcUsd);
    emit changed();
    if (m_hasWallet)
        saveCache();
}

void WalletController::setCurrency(const QString &code)
{
    const QString next = code.trimmed().toUpper();
    if (next.isEmpty() || next == m_currency)
        return;
    m_currency = next;
    m_btcUsd = 0;
    QSettings settings;
    settings.setValue(QStringLiteral("currency"), m_currency);
    emit changed();
}

void WalletController::setDisplayName(const QString &name)
{
    const QString next = name.trimmed();
    if (next == m_displayName)
        return;
    m_displayName = next;
    QSettings settings;
    settings.setValue(QStringLiteral("displayName"), m_displayName);
    emit changed();
}

void WalletController::loadVtxos()
{
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("vtxos"));
    enqueue(req);
}

static QJsonArray vtxoIdArray(const QString &idsJson, QString *error)
{
    QJsonParseError parseError;
    const QJsonDocument doc = QJsonDocument::fromJson(idsJson.toUtf8(), &parseError);
    if (parseError.error != QJsonParseError::NoError || !doc.isArray()) {
        if (error)
            *error = QStringLiteral("Could not read the selected VTXOs.");
        return QJsonArray();
    }
    QJsonArray out;
    const QJsonArray raw = doc.array();
    for (int i = 0; i < raw.size(); ++i) {
        const QString id = raw.at(i).toString().trimmed();
        if (!id.isEmpty())
            out.append(id);
    }
    if (out.isEmpty() && error)
        *error = QStringLiteral("Select at least one VTXO.");
    return out;
}

void WalletController::loadRefreshDue()
{
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("refresh_due"));
    enqueue(req);
}

void WalletController::estimateRefresh(const QString &idsJson)
{
    QString problem;
    const QJsonArray list = vtxoIdArray(idsJson, &problem);
    m_hasRefreshFee = false;
    m_refreshGross = 0;
    m_refreshFee = 0;
    m_refreshNet = 0;
    if (list.isEmpty()) {
        m_lastOp = QStringLiteral("refresh_fee");
        m_error = problem;
        emit changed();
        return;
    }
    m_error.clear();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("refresh_fee"));
    req.insert(QStringLiteral("vtxo_ids"), list);
    enqueue(req);
}

void WalletController::refreshVtxos(const QString &idsJson)
{
    QString problem;
    const QJsonArray list = vtxoIdArray(idsJson, &problem);
    m_refreshScheduled = false;
    m_refreshDetail.clear();
    if (list.isEmpty()) {
        m_lastOp = QStringLiteral("refresh_vtxos");
        m_error = problem;
        emit changed();
        return;
    }
    m_error.clear();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("refresh_vtxos"));
    req.insert(QStringLiteral("vtxo_ids"), list);
    enqueue(req);
}

void WalletController::loadBoardInfo()
{
    m_boardTxid.clear();
    m_boardAmount = 0;
    emit changed();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("ark_info"));
    enqueue(req);
}

void WalletController::estimatePay(const QString &method, const QString &destination, quint64 sats)
{
    m_hasPayFee = false;
    m_payFeeMethod = method;
    m_payGross = 0;
    m_payFee = 0;
    m_payNet = 0;
    emit changed();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("pay_fee"));
    req.insert(QStringLiteral("method"), method);
    req.insert(QStringLiteral("uri"), destination);
    req.insert(QStringLiteral("amount_sat"), QJsonValue::fromVariant(sats));
    enqueue(req);
}

void WalletController::lookupLnurl(const QString &text)
{
    const QString link = text.trimmed();
    if (link.isEmpty())
        return;
    if (link == m_lnurlTarget && m_lnurlReady)
        return;
    m_lnurlTarget = link;
    m_lnurlReady = false;
    m_lnurlFixed = false;
    m_lnurlAmount = 0;
    emit changed();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("lnurl"));
    req.insert(QStringLiteral("uri"), link);
    enqueue(req);
}

void WalletController::estimateBoard(quint64 sats)
{
    m_hasBoardFee = false;
    m_boardGross = 0;
    m_boardFee = 0;
    m_boardNet = 0;
    emit changed();
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("board_fee"));
    req.insert(QStringLiteral("amount_sat"), QJsonValue::fromVariant(sats));
    enqueue(req);
}

void WalletController::accelPreview(const QString &txid)
{
    const QString id = txid.trimmed().toLower();
    if (id.isEmpty())
        return;
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("accel_preview"));
    req.insert(QStringLiteral("txid"), id);
    enqueue(req);
}

void WalletController::accelInvoice(const QString &txid, quint64 maxBid)
{
    const QString id = txid.trimmed().toLower();
    if (id.isEmpty() || maxBid == 0)
        return;
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("accel_invoice"));
    req.insert(QStringLiteral("txid"), id);
    req.insert(QStringLiteral("max_bid"), QJsonValue::fromVariant(maxBid));
    enqueue(req);
}

void WalletController::board(quint64 sats)
{
    m_boardTxid.clear();
    m_boardAmount = 0;
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("board"));
    req.insert(QStringLiteral("amount_sat"), QJsonValue::fromVariant(sats));
    enqueue(req);
}

void WalletController::wipe()
{
    disconnect(&m_proc, 0, this, 0);
    m_queue.clear();
    m_waiting = false;
    if (m_proc.state() != QProcess::NotRunning) {
        m_proc.kill();
        m_proc.waitForFinished(3000);
    }
    QDir(m_datadir).removeRecursively();
}

void WalletController::pay(const QString &uri)
{
    const QString text = uri.trimmed();
    if (text.isEmpty() || m_paying)
        return;
    m_paying = true;
    QJsonObject req;
    req.insert(QStringLiteral("op"), QStringLiteral("pay"));
    req.insert(QStringLiteral("uri"), text);
    enqueue(req);
}

void WalletController::apply(const QJsonObject &resp)
{
    m_ready = true;
    m_lastOp = resp.value(QStringLiteral("op")).toString();
    const QString opError = resp.value(QStringLiteral("error")).toString();
    m_error = opError;
    if (m_lastOp == QStringLiteral("lnurl") && resp.value(QStringLiteral("ok")).toBool()) {
        const QString link = resp.value(QStringLiteral("lnurl")).toString();
        if (link == m_lnurlTarget) {
            m_lnurlReady = true;
            m_lnurlFixed = resp.value(QStringLiteral("lnurl_fixed")).toBool();
            m_lnurlAmount = resp.value(QStringLiteral("lnurl_amount")).toVariant().toULongLong();
        }
    }
    if (resp.contains(QStringLiteral("has_wallet"))) {
        m_hasWallet = resp.value(QStringLiteral("has_wallet")).toBool();
        if (!m_hasWallet)
            QFile::remove(cachePath());
    }
    if (resp.value(QStringLiteral("ok")).toBool()) {
        const QString mnemonic = resp.value(QStringLiteral("mnemonic")).toString();
        if (!mnemonic.isEmpty())
            m_mnemonic = mnemonic;
        m_fingerprint = resp.value(QStringLiteral("fingerprint")).toString();
        m_ark = resp.value(QStringLiteral("ark")).toString();
        m_onchain = resp.value(QStringLiteral("onchain")).toString();
        m_bolt11 = resp.value(QStringLiteral("bolt11")).toString();
        m_bip321 = resp.value(QStringLiteral("bip321")).toString();
        if (resp.contains(QStringLiteral("can_ark")))
            m_canArk = resp.value(QStringLiteral("can_ark")).toBool();
        if (resp.contains(QStringLiteral("can_lightning")))
            m_canLightning = resp.value(QStringLiteral("can_lightning")).toBool();
        if (resp.contains(QStringLiteral("can_onchain")))
            m_canOnchain = resp.value(QStringLiteral("can_onchain")).toBool();
        if (resp.contains(QStringLiteral("can_all")))
            m_canAll = resp.value(QStringLiteral("can_all")).toBool();
        const QString png = resp.value(QStringLiteral("qr_png")).toString();
        if (!png.isEmpty() && png != m_qrPng) {
            const QString path = m_datadir + QStringLiteral("/receive-qr.png");
            QFile file(path);
            if (file.open(QIODevice::WriteOnly)) {
                file.write(QByteArray::fromBase64(png.toLatin1()));
                file.close();
                m_qrPng = png;
                m_qrImage = QUrl::fromLocalFile(path).toString()
                        + QStringLiteral("?t=")
                        + QString::number(QDateTime::currentMSecsSinceEpoch());
            }
        }
        const bool authoritative = m_lastOp == QStringLiteral("sync")
                || m_lastOp == QStringLiteral("pay")
                || m_lastOp == QStringLiteral("import")
                || m_lastOp == QStringLiteral("board");
        const bool incomingEmpty = resp.value(QStringLiteral("total_sats")).toVariant().toULongLong() == 0
                && resp.value(QStringLiteral("history")).toArray().isEmpty();
        const bool keepBalances = m_fromCache && !authoritative && incomingEmpty;
        if (!keepBalances) {
            m_spendable = resp.value(QStringLiteral("spendable_sats")).toVariant().toULongLong();
            m_total = resp.value(QStringLiteral("total_sats")).toVariant().toULongLong();
            m_onchainTotal = resp.value(QStringLiteral("onchain_total")).toVariant().toULongLong();
            m_onchainConfirmed = resp.value(QStringLiteral("onchain_confirmed")).toVariant().toULongLong();
            m_onchainPending = resp.value(QStringLiteral("onchain_pending")).toVariant().toULongLong();
            m_offchainTotal = resp.value(QStringLiteral("offchain_total")).toVariant().toULongLong();
            m_pendingSend = resp.value(QStringLiteral("pending_send")).toVariant().toULongLong();
            m_pendingInRound = resp.value(QStringLiteral("pending_in_round")).toVariant().toULongLong();
            m_pendingExit = resp.value(QStringLiteral("pending_exit")).toVariant().toULongLong();
            m_pendingBoard = resp.value(QStringLiteral("pending_board")).toVariant().toULongLong();
            m_claimableReceive = resp.value(QStringLiteral("claimable_receive")).toVariant().toULongLong();
            if (resp.contains(QStringLiteral("history"))) {
                m_history = resp.value(QStringLiteral("history")).toArray().toVariantList();
                rememberHistory();
            }
        }
        if (authoritative)
            m_fromCache = false;
        const QString paid = resp.value(QStringLiteral("pay_result")).toString();
        if (!paid.isEmpty())
            m_payResult = paid;
        if (m_lastOp == QStringLiteral("vtxos")) {
            m_vtxos = resp.value(QStringLiteral("vtxos")).toArray().toVariantList();
            m_vtxoExitDelta = resp.value(QStringLiteral("vtxo_exit_delta")).toVariant().toUInt();
            m_minBoard = resp.value(QStringLiteral("min_board")).toVariant().toULongLong();
            m_vtxoSerial++;
        }
        if (resp.contains(QStringLiteral("tip_height"))) {
            const quint32 tip = resp.value(QStringLiteral("tip_height")).toVariant().toUInt();
            if (tip > 0)
                m_tipHeight = tip;
        }
        if (resp.contains(QStringLiteral("refresh_count"))) {
            m_refreshCount = resp.value(QStringLiteral("refresh_count")).toInt();
            m_refreshIds = resp.value(QStringLiteral("refresh_ids")).toArray().toVariantList();
            m_refreshSats = resp.value(QStringLiteral("refresh_sats")).toVariant().toULongLong();
            m_refreshHeight = resp.value(QStringLiteral("refresh_height")).toVariant().toUInt();
            m_refreshInRound = resp.value(QStringLiteral("refresh_in_round")).toInt();
            m_roundSummary = resp.value(QStringLiteral("round_summary")).toString();
        }
        if (m_lastOp == QStringLiteral("ark_info") || m_lastOp == QStringLiteral("board_fee")
                || m_lastOp == QStringLiteral("vtxos")) {
            if (resp.contains(QStringLiteral("min_board")))
                m_minBoard = resp.value(QStringLiteral("min_board")).toVariant().toULongLong();
        }
        if (m_lastOp == QStringLiteral("board_fee")) {
            m_hasBoardFee = resp.value(QStringLiteral("has_board_fee")).toBool();
            m_boardGross = resp.value(QStringLiteral("board_gross")).toVariant().toULongLong();
            m_boardFee = resp.value(QStringLiteral("board_fee")).toVariant().toULongLong();
            m_boardNet = resp.value(QStringLiteral("board_net")).toVariant().toULongLong();
        }
        if (m_lastOp == QStringLiteral("board")) {
            m_boardTxid = resp.value(QStringLiteral("board_txid")).toString();
            m_boardAmount = resp.value(QStringLiteral("board_amount")).toVariant().toULongLong();
        }
        if (m_lastOp == QStringLiteral("accel_preview") || m_lastOp == QStringLiteral("accel_invoice")) {
            QJsonObject accel = resp.value(QStringLiteral("accelerator")).toObject();
            const QString png = accel.value(QStringLiteral("qr_png")).toString();
            accel.remove(QStringLiteral("qr_png"));
            m_acceleratorJson = QString::fromUtf8(QJsonDocument(accel).toJson(QJsonDocument::Compact));
            if (m_lastOp == QStringLiteral("accel_preview"))
                m_accelQrImage.clear();
            if (!png.isEmpty()) {
                const QString path = m_datadir + QStringLiteral("/accel-qr.png");
                QFile file(path);
                if (file.open(QIODevice::WriteOnly)) {
                    file.write(QByteArray::fromBase64(png.toLatin1()));
                    file.close();
                    m_accelQrImage = QUrl::fromLocalFile(path).toString()
                            + QStringLiteral("?t=")
                            + QString::number(QDateTime::currentMSecsSinceEpoch());
                }
            }
        }
        if (m_hasWallet && (authoritative || m_lastOp == QStringLiteral("uri")))
            saveCache();
    } else if (!opError.isEmpty() && m_lastOp == QStringLiteral("pay")) {
        m_payResult = opError;
    }
    if (m_lastOp == QStringLiteral("refresh_fee")) {
        m_hasRefreshFee = resp.value(QStringLiteral("ok")).toBool()
                && resp.value(QStringLiteral("has_refresh_fee")).toBool();
        m_refreshGross = m_hasRefreshFee ? resp.value(QStringLiteral("refresh_gross")).toVariant().toULongLong() : 0;
        m_refreshFee = m_hasRefreshFee ? resp.value(QStringLiteral("refresh_fee")).toVariant().toULongLong() : 0;
        m_refreshNet = m_hasRefreshFee ? resp.value(QStringLiteral("refresh_net")).toVariant().toULongLong() : 0;
    }
    if (m_lastOp == QStringLiteral("refresh_vtxos")) {
        m_refreshScheduled = resp.value(QStringLiteral("ok")).toBool()
                && resp.value(QStringLiteral("refresh_scheduled")).toBool();
        m_refreshDetail = resp.value(QStringLiteral("refresh_detail")).toString();
    }
    if (m_lastOp == QStringLiteral("pay_fee")) {
        if (resp.contains(QStringLiteral("pay_method")))
            m_payFeeMethod = resp.value(QStringLiteral("pay_method")).toString();
        m_hasPayFee = resp.value(QStringLiteral("ok")).toBool()
                && resp.value(QStringLiteral("has_pay_fee")).toBool();
        m_payGross = m_hasPayFee ? resp.value(QStringLiteral("pay_gross")).toVariant().toULongLong() : 0;
        m_payFee = m_hasPayFee ? resp.value(QStringLiteral("pay_fee")).toVariant().toULongLong() : 0;
        m_payNet = m_hasPayFee ? resp.value(QStringLiteral("pay_net")).toVariant().toULongLong() : 0;
    }
    if (m_lastOp == QStringLiteral("pay"))
        m_paying = false;
    emit changed();
}

void WalletController::onReadyRead()
{
    m_buf += m_proc.readAllStandardOutput();
    while (true) {
        const int nl = m_buf.indexOf('\n');
        if (nl < 0)
            break;
        const QByteArray line = m_buf.left(nl);
        m_buf.remove(0, nl + 1);
        const QJsonDocument doc = QJsonDocument::fromJson(line);
        if (doc.isObject())
            apply(doc.object());
        m_waiting = false;
        if (m_queue.isEmpty())
            setBusy(false);
        pump();
    }
}

void WalletController::onReadyReadStderr()
{
    m_errBuf += m_proc.readAllStandardError();
    while (true) {
        const int nl = m_errBuf.indexOf('\n');
        if (nl < 0)
            break;
        const QString line = QString::fromUtf8(m_errBuf.left(nl)).trimmed();
        m_errBuf.remove(0, nl + 1);
        const QString prefix = QStringLiteral("progress:");
        if (line.startsWith(prefix))
            setActivity(line.mid(prefix.size()).trimmed());
        else if (line == QStringLiteral("event:movement"))
            m_movementTimer.start(400);
    }
}

void WalletController::syncFromWatch()
{
    if (!m_hasWallet)
        return;
    m_announce = true;
    QQueue<QJsonObject> queued = m_queue;
    while (!queued.isEmpty()) {
        if (queued.dequeue().value(QStringLiteral("op")).toString() == QStringLiteral("sync"))
            return;
    }
    sync();
}

void WalletController::onFinished(int code, QProcess::ExitStatus status)
{
    Q_UNUSED(status);
    m_waiting = false;
    m_paying = false;
    m_ready = true;
    m_queue.clear();
    setBusy(false);
    if (m_error.isEmpty())
        m_error = QStringLiteral("wallet helper exited (%1)").arg(code);
    emit changed();
}
