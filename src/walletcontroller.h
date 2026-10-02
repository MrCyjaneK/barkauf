#ifndef WALLETCONTROLLER_H
#define WALLETCONTROLLER_H

#include <QObject>
#include <QProcess>
#include <QQueue>
#include <QJsonObject>
#include <QString>
#include <QTimer>
#include <QVariantList>

class WalletController : public QObject
{
    Q_OBJECT
    Q_PROPERTY(bool busy READ busy NOTIFY busyChanged)
    Q_PROPERTY(bool ready READ ready NOTIFY changed)
    Q_PROPERTY(bool hasWallet READ hasWallet NOTIFY changed)
    Q_PROPERTY(QString lastOp READ lastOp NOTIFY changed)
    Q_PROPERTY(QString error READ error NOTIFY changed)
    Q_PROPERTY(QString activity READ activity NOTIFY changed)
    Q_PROPERTY(QString mnemonic READ mnemonic NOTIFY changed)
    Q_PROPERTY(QString fingerprint READ fingerprint NOTIFY changed)
    Q_PROPERTY(QString ark READ ark NOTIFY changed)
    Q_PROPERTY(QString onchain READ onchain NOTIFY changed)
    Q_PROPERTY(QString bolt11 READ bolt11 NOTIFY changed)
    Q_PROPERTY(QString bip321 READ bip321 NOTIFY changed)
    Q_PROPERTY(bool canArk READ canArk NOTIFY changed)
    Q_PROPERTY(bool canLightning READ canLightning NOTIFY changed)
    Q_PROPERTY(bool canOnchain READ canOnchain NOTIFY changed)
    Q_PROPERTY(bool canAll READ canAll NOTIFY changed)
    Q_PROPERTY(QString qrImage READ qrImage NOTIFY changed)
    Q_PROPERTY(QString payResult READ payResult NOTIFY changed)
    Q_PROPERTY(quint64 spendableSats READ spendableSats NOTIFY changed)
    Q_PROPERTY(quint64 totalSats READ totalSats NOTIFY changed)
    Q_PROPERTY(quint64 onchainTotal READ onchainTotal NOTIFY changed)
    Q_PROPERTY(quint64 onchainConfirmed READ onchainConfirmed NOTIFY changed)
    Q_PROPERTY(quint64 onchainPending READ onchainPending NOTIFY changed)
    Q_PROPERTY(quint64 offchainTotal READ offchainTotal NOTIFY changed)
    Q_PROPERTY(quint64 pendingSend READ pendingSend NOTIFY changed)
    Q_PROPERTY(quint64 pendingInRound READ pendingInRound NOTIFY changed)
    Q_PROPERTY(quint64 pendingExit READ pendingExit NOTIFY changed)
    Q_PROPERTY(quint64 pendingBoard READ pendingBoard NOTIFY changed)
    Q_PROPERTY(quint64 claimableReceive READ claimableReceive NOTIFY changed)
    Q_PROPERTY(QVariantList history READ history NOTIFY changed)
    Q_PROPERTY(quint64 amountSat READ amountSat NOTIFY changed)
    Q_PROPERTY(double btcUsd READ btcUsd NOTIFY changed)
    Q_PROPERTY(QString currency READ currency NOTIFY changed)
    Q_PROPERTY(QString rateCurrency READ rateCurrency NOTIFY changed)
    Q_PROPERTY(QString displayName READ displayName NOTIFY changed)
    Q_PROPERTY(QVariantList vtxos READ vtxos NOTIFY changed)
    Q_PROPERTY(quint32 vtxoSerial READ vtxoSerial NOTIFY changed)
    Q_PROPERTY(quint32 tipHeight READ tipHeight NOTIFY changed)
    Q_PROPERTY(quint32 vtxoExitDelta READ vtxoExitDelta NOTIFY changed)
    Q_PROPERTY(quint64 minBoard READ minBoard NOTIFY changed)
    Q_PROPERTY(bool hasBoardFee READ hasBoardFee NOTIFY changed)
    Q_PROPERTY(quint64 boardGross READ boardGross NOTIFY changed)
    Q_PROPERTY(quint64 boardFee READ boardFee NOTIFY changed)
    Q_PROPERTY(quint64 boardNet READ boardNet NOTIFY changed)
    Q_PROPERTY(QString boardTxid READ boardTxid NOTIFY changed)
    Q_PROPERTY(quint64 boardAmount READ boardAmount NOTIFY changed)
    Q_PROPERTY(bool hasPayFee READ hasPayFee NOTIFY changed)
    Q_PROPERTY(QString payFeeMethod READ payFeeMethod NOTIFY changed)
    Q_PROPERTY(quint64 payGross READ payGross NOTIFY changed)
    Q_PROPERTY(quint64 payFee READ payFee NOTIFY changed)
    Q_PROPERTY(quint64 payNet READ payNet NOTIFY changed)
    Q_PROPERTY(bool lnurlReady READ lnurlReady NOTIFY changed)
    Q_PROPERTY(bool lnurlFixed READ lnurlFixed NOTIFY changed)
    Q_PROPERTY(quint64 lnurlAmount READ lnurlAmount NOTIFY changed)
    Q_PROPERTY(QString lnurlTarget READ lnurlTarget NOTIFY changed)
    Q_PROPERTY(QString acceleratorJson READ acceleratorJson NOTIFY changed)
    Q_PROPERTY(QString accelQrImage READ accelQrImage NOTIFY changed)
    Q_PROPERTY(int refreshCount READ refreshCount NOTIFY changed)
    Q_PROPERTY(quint64 refreshSats READ refreshSats NOTIFY changed)
    Q_PROPERTY(quint32 refreshHeight READ refreshHeight NOTIFY changed)
    Q_PROPERTY(QVariantList refreshIds READ refreshIds NOTIFY changed)
    Q_PROPERTY(bool hasRefreshFee READ hasRefreshFee NOTIFY changed)
    Q_PROPERTY(quint64 refreshGross READ refreshGross NOTIFY changed)
    Q_PROPERTY(quint64 refreshFee READ refreshFee NOTIFY changed)
    Q_PROPERTY(quint64 refreshNet READ refreshNet NOTIFY changed)
    Q_PROPERTY(bool refreshScheduled READ refreshScheduled NOTIFY changed)
    Q_PROPERTY(QString refreshDetail READ refreshDetail NOTIFY changed)
    Q_PROPERTY(int refreshInRound READ refreshInRound NOTIFY changed)
    Q_PROPERTY(QString roundSummary READ roundSummary NOTIFY changed)

public:
    explicit WalletController(QObject *parent = 0);
    ~WalletController();

    bool busy() const { return m_busy; }
    bool ready() const { return m_ready; }
    bool hasWallet() const { return m_hasWallet; }
    QString lastOp() const { return m_lastOp; }
    QString error() const { return m_error; }
    QString activity() const { return m_activity; }
    QString mnemonic() const { return m_mnemonic; }
    QString fingerprint() const { return m_fingerprint; }
    QString ark() const { return m_ark; }
    QString onchain() const { return m_onchain; }
    QString bolt11() const { return m_bolt11; }
    QString bip321() const { return m_bip321; }
    bool canArk() const { return m_canArk; }
    bool canLightning() const { return m_canLightning; }
    bool canOnchain() const { return m_canOnchain; }
    bool canAll() const { return m_canAll; }
    QString qrImage() const { return m_qrImage; }
    QString payResult() const { return m_payResult; }
    quint64 spendableSats() const { return m_spendable; }
    quint64 totalSats() const { return m_total; }
    quint64 onchainTotal() const { return m_onchainTotal; }
    quint64 onchainConfirmed() const { return m_onchainConfirmed; }
    quint64 onchainPending() const { return m_onchainPending; }
    quint64 offchainTotal() const { return m_offchainTotal; }
    quint64 pendingSend() const { return m_pendingSend; }
    quint64 pendingInRound() const { return m_pendingInRound; }
    quint64 pendingExit() const { return m_pendingExit; }
    quint64 pendingBoard() const { return m_pendingBoard; }
    quint64 claimableReceive() const { return m_claimableReceive; }
    QVariantList history() const { return m_history; }
    quint64 amountSat() const { return m_amountSat; }
    double btcUsd() const { return m_btcUsd; }
    QString currency() const { return m_currency; }
    QString rateCurrency() const { return m_rateCurrency; }
    QString displayName() const { return m_displayName; }
    QVariantList vtxos() const { return m_vtxos; }
    quint32 vtxoSerial() const { return m_vtxoSerial; }
    quint32 tipHeight() const { return m_tipHeight; }
    quint32 vtxoExitDelta() const { return m_vtxoExitDelta; }
    quint64 minBoard() const { return m_minBoard; }
    bool hasBoardFee() const { return m_hasBoardFee; }
    quint64 boardGross() const { return m_boardGross; }
    quint64 boardFee() const { return m_boardFee; }
    quint64 boardNet() const { return m_boardNet; }
    QString boardTxid() const { return m_boardTxid; }
    quint64 boardAmount() const { return m_boardAmount; }
    bool hasPayFee() const { return m_hasPayFee; }
    QString payFeeMethod() const { return m_payFeeMethod; }
    quint64 payGross() const { return m_payGross; }
    quint64 payFee() const { return m_payFee; }
    quint64 payNet() const { return m_payNet; }
    bool lnurlReady() const { return m_lnurlReady; }
    bool lnurlFixed() const { return m_lnurlFixed; }
    quint64 lnurlAmount() const { return m_lnurlAmount; }
    QString lnurlTarget() const { return m_lnurlTarget; }
    QString acceleratorJson() const {
        return m_acceleratorJson.isEmpty() ? QStringLiteral("{}") : m_acceleratorJson;
    }
    QString accelQrImage() const { return m_accelQrImage; }
    int refreshCount() const { return m_refreshCount; }
    quint64 refreshSats() const { return m_refreshSats; }
    quint32 refreshHeight() const { return m_refreshHeight; }
    QVariantList refreshIds() const { return m_refreshIds; }
    bool hasRefreshFee() const { return m_hasRefreshFee; }
    quint64 refreshGross() const { return m_refreshGross; }
    quint64 refreshFee() const { return m_refreshFee; }
    quint64 refreshNet() const { return m_refreshNet; }
    bool refreshScheduled() const { return m_refreshScheduled; }
    QString refreshDetail() const { return m_refreshDetail; }
    int refreshInRound() const { return m_refreshInRound; }
    QString roundSummary() const { return m_roundSummary; }

    Q_INVOKABLE void refresh();
    Q_INVOKABLE void generate();
    Q_INVOKABLE void importMnemonic(const QString &mnemonic);
    Q_INVOKABLE void sync();
    Q_INVOKABLE void setAmount(quint64 sats);
    Q_INVOKABLE void pay(const QString &uri);
    Q_INVOKABLE void wipe();
    Q_INVOKABLE void rememberRate(double usdPerBtc);
    Q_INVOKABLE void setCurrency(const QString &code);
    Q_INVOKABLE void setDisplayName(const QString &name);
    Q_INVOKABLE void loadVtxos();
    Q_INVOKABLE void loadRefreshDue();
    Q_INVOKABLE void estimateRefresh(const QString &idsJson);
    Q_INVOKABLE void refreshVtxos(const QString &idsJson);
    Q_INVOKABLE void loadBoardInfo();
    Q_INVOKABLE void estimateBoard(quint64 sats);
    Q_INVOKABLE void estimatePay(const QString &method, const QString &destination, quint64 sats);
    Q_INVOKABLE void lookupLnurl(const QString &text);
    Q_INVOKABLE void board(quint64 sats);
    Q_INVOKABLE void accelPreview(const QString &txid);
    Q_INVOKABLE void accelInvoice(const QString &txid, quint64 maxBid);

signals:
    void busyChanged();
    void changed();

private slots:
    void onReadyRead();
    void onReadyReadStderr();
    void onFinished(int code, QProcess::ExitStatus status);
    void syncFromWatch();
    void shutdown();

private:
    void ensureStarted();
    void enqueue(const QJsonObject &req);
    void pump();
    void apply(const QJsonObject &resp);
    void setBusy(bool busy);
    void setActivity(const QString &text);
    void rememberHistory();
    void publishNotification(const QString &summary, const QString &body);
    void loadCache();
    void saveCache();
    QString cachePath() const;
    QString binaryPath() const;

    QProcess m_proc;
    QTimer m_movementTimer;
    QByteArray m_buf;
    QQueue<QJsonObject> m_queue;
    bool m_waiting;
    bool m_busy;
    bool m_ready;
    bool m_hasWallet;
    bool m_paying;
    bool m_fromCache;
    QString m_currentOp;
    QString m_lastOp;
    QString m_error;
    QString m_activity;
    QByteArray m_errBuf;
    QString m_mnemonic;
    QString m_fingerprint;
    QString m_ark;
    QString m_onchain;
    QString m_bolt11;
    QString m_bip321;
    bool m_canArk;
    bool m_canLightning;
    bool m_canOnchain;
    bool m_canAll;
    QString m_qrImage;
    QString m_qrPng;
    QString m_payResult;
    quint64 m_spendable;
    quint64 m_total;
    quint64 m_onchainTotal;
    quint64 m_onchainConfirmed;
    quint64 m_onchainPending;
    quint64 m_offchainTotal;
    quint64 m_pendingSend;
    quint64 m_pendingInRound;
    quint64 m_pendingExit;
    quint64 m_pendingBoard;
    quint64 m_claimableReceive;
    QVariantList m_history;
    bool m_historyReady;
    bool m_announce;
    bool m_stopped;
    QString m_lastHistoryId;
    quint64 m_amountSat;
    double m_btcUsd;
    QString m_currency;
    QString m_rateCurrency;
    QString m_displayName;
    QVariantList m_vtxos;
    quint32 m_vtxoSerial;
    quint32 m_tipHeight;
    quint32 m_vtxoExitDelta;
    quint64 m_minBoard;
    bool m_hasBoardFee;
    quint64 m_boardGross;
    quint64 m_boardFee;
    quint64 m_boardNet;
    QString m_boardTxid;
    quint64 m_boardAmount;
    bool m_hasPayFee;
    QString m_payFeeMethod;
    quint64 m_payGross;
    quint64 m_payFee;
    quint64 m_payNet;
    bool m_lnurlReady;
    bool m_lnurlFixed;
    quint64 m_lnurlAmount;
    QString m_lnurlTarget;
    QString m_acceleratorJson;
    QString m_accelQrImage;
    int m_refreshCount;
    quint64 m_refreshSats;
    quint32 m_refreshHeight;
    QVariantList m_refreshIds;
    bool m_hasRefreshFee;
    quint64 m_refreshGross;
    quint64 m_refreshFee;
    quint64 m_refreshNet;
    bool m_refreshScheduled;
    QString m_refreshDetail;
    int m_refreshInRound;
    QString m_roundSummary;
    QString m_datadir;
};

#endif
