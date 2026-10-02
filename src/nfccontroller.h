#ifndef NFCCONTROLLER_H
#define NFCCONTROLLER_H

#include <QObject>
#include <QDBusMessage>
#include <QByteArray>
#include <QString>

class QDBusInterface;

class NfcController : public QObject
{
    Q_OBJECT
    Q_PROPERTY(bool enabled READ enabled NOTIFY changed)
    Q_PROPERTY(bool powered READ powered NOTIFY changed)
    Q_PROPERTY(bool sharing READ sharing NOTIFY changed)
    Q_PROPERTY(QString statusText READ statusText NOTIFY changed)
    Q_PROPERTY(QString lastRead READ lastRead NOTIFY changed)
    Q_PROPERTY(QString shareText READ shareText WRITE setShareText NOTIFY changed)

public:
    explicit NfcController(QObject *parent = 0);
    ~NfcController();

    bool enabled() const { return m_enabled; }
    bool powered() const { return m_powered; }
    bool sharing() const { return m_sharing; }
    QString statusText() const { return m_status; }
    QString lastRead() const { return m_lastRead; }
    QString shareText() const { return m_shareText; }
    void setShareText(const QString &text);

    Q_INVOKABLE void start();
    Q_INVOKABLE void stop();
    Q_INVOKABLE void setSharing(bool on);

    QByteArray readBinary(int offset, int le, unsigned char *sw1, unsigned char *sw2, unsigned *responseId);
    void selectFile(const QByteArray &fid, unsigned char *sw1, unsigned char *sw2);
    void resetSelection();

signals:
    void changed();
    void textRead(const QString &text);

private slots:
    void onTagsChanged(const QDBusMessage &message);

private:
    void refreshAdapter();
    void requestMode(unsigned enable, unsigned *id);
    void releaseMode(unsigned *id);
    void readTag(const QString &path);
    void rebuildNdef();
    void setStatus(const QString &text);

    bool m_started;
    bool m_enabled;
    bool m_powered;
    bool m_sharing;
    unsigned m_readerMode;
    unsigned m_shareMode;
    QString m_adapter;
    QString m_status;
    QString m_lastRead;
    QString m_lastPaid;
    QString m_shareText;
    QByteArray m_cc;
    QByteArray m_ndef;
    int m_selected;
    QDBusInterface *m_daemon;
};

#endif
