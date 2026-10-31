#include "gitsource.h"

#include <QFileInfo>
#include <QObject>
#include <QProcess>
#include <QStringList>

namespace {

bool runProcess(const QString &program, const QStringList &arguments, const QString &workingDirectory,
                QByteArray *output, QString *error)
{
    QProcess process;
    process.setWorkingDirectory(workingDirectory);
    process.start(program, arguments);
    if (!process.waitForStarted(5000)) {
        if (error)
            *error = QObject::tr("Cannot start %1: %2").arg(program, process.errorString());
        return false;
    }
    if (!process.waitForFinished(60000)) {
        process.kill();
        process.waitForFinished();
        if (error)
            *error = QObject::tr("%1 did not finish in time.").arg(program);
        return false;
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        QString details = QString::fromLocal8Bit(process.readAllStandardError()).trimmed();
        if (details.isEmpty())
            details = QObject::tr("exit code %1").arg(process.exitCode());
        if (error)
            *error = QObject::tr("%1 failed: %2").arg(program, details);
        return false;
    }
    if (output)
        *output = process.readAllStandardOutput();
    return true;
}

bool runGit(const QString &repo, const QStringList &arguments, QByteArray *output, QString *error)
{
    return runProcess(QStringLiteral("git"), QStringList{QStringLiteral("-C"), repo} + arguments,
                      QString(), output, error);
}

} // namespace

namespace GitSource {

bool isRepositoryRoot(const QString &dir, QString *error)
{
    QByteArray output;
    if (!runGit(dir, {QStringLiteral("rev-parse"), QStringLiteral("--show-toplevel")}, &output, error)) {
        if (error)
            *error = QObject::tr("%1 is not inside a Git work tree: %2")
                         .arg(dir, *error);
        return false;
    }

    const QString top = QFileInfo(QString::fromLocal8Bit(output).trimmed()).canonicalFilePath();
    const QString source = QFileInfo(dir).canonicalFilePath();
    if (top.isEmpty() || source.isEmpty() || top != source) {
        if (error)
            *error = QObject::tr("%1 is not the top level of a Git work tree (top level: %2).")
                         .arg(dir, top.isEmpty() ? QStringLiteral("?") : top);
        return false;
    }
    return true;
}

bool writeTrackedFileList(const QString &repo, bool includeRepository, QIODevice *out, QString *error)
{
    QByteArray tracked;
    if (!runGit(repo, {QStringLiteral("ls-files"), QStringLiteral("-z")}, &tracked, error))
        return false;

    if (out->write(tracked) < 0) {
        if (error)
            *error = QObject::tr("Cannot write the Git file list: %1").arg(out->errorString());
        return false;
    }

    if (includeRepository) {
        const QString gitDir = repo + QStringLiteral("/.git");
        const QFileInfo gitInfo(gitDir);
        if (gitInfo.isFile()) {
            // Linked worktrees and submodules have a .git file instead of a directory.
            if (out->write(".git\0", 5) < 0) {
                if (error)
                    *error = QObject::tr("Cannot write the Git file list: %1").arg(out->errorString());
                return false;
            }
        } else if (gitInfo.isDir()) {
            QByteArray entries;
            if (!runProcess(QStringLiteral("find"),
                            {QStringLiteral(".git"), QStringLiteral("-print0")},
                            repo, &entries, error)) {
                if (error)
                    *error = QObject::tr("Cannot list the .git repository: %1").arg(*error);
                return false;
            }
            if (out->write(entries) < 0) {
                if (error)
                    *error = QObject::tr("Cannot write the Git file list: %1").arg(out->errorString());
                return false;
            }
        } else {
            if (error)
                *error = QObject::tr("The .git repository was not found in %1.").arg(repo);
            return false;
        }
    }

    if (tracked.isEmpty() && !includeRepository) {
        if (error)
            *error = QObject::tr("No files tracked by Git were found in %1.").arg(repo);
        return false;
    }
    return true;
}

} // namespace GitSource
