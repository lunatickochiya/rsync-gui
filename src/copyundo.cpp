#include "copyundo.h"
#include "localpath.h"

#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>

#include <algorithm>
#include <cerrno>
#include <cstring>
#include <functional>
#include <vector>

#include <dirent.h>
#include <sys/stat.h>
#include <unistd.h>

QString UndoRecord::undoDir() const
{
    if (!valid())
        return QString();
    return destination + QLatin1Char('/') + undoDirName;
}

QString UndoRecord::recordPath() const
{
    return undoDir().isEmpty() ? QString() : undoDir() + QLatin1Char('/') + CopyUndo::recordFileName();
}

QString CopyUndo::recordFileName()
{
    return QStringLiteral("rsync-gui-undo.json");
}

namespace {

struct Entry
{
    QByteArray path; // raw bytes
    QString name;    // one path component, for messages
};

// The entries of a directory, without "." and "..".
QList<Entry> listDir(const QByteArray &dir)
{
    QList<Entry> entries;
    if (DIR *d = ::opendir(dir.constData())) {
        while (dirent *entry = ::readdir(d)) {
            if (std::strcmp(entry->d_name, ".") == 0 || std::strcmp(entry->d_name, "..") == 0)
                continue;
            entries << Entry{dir + '/' + entry->d_name, LocalPath::decode(entry->d_name)};
        }
        ::closedir(d);
    }
    return entries;
}

bool lstatPath(const QByteArray &path, struct stat *st)
{
    return ::lstat(path.constData(), st) == 0;
}

bool isRealDir(const QByteArray &path)
{
    struct stat st;
    return lstatPath(path, &st) && S_ISDIR(st.st_mode);
}

// A name recorded by rsync (relative to the destination) that is safe to join onto it.
bool isSafeRelativeName(const QString &name)
{
    if (name.isEmpty() || name.startsWith(QLatin1Char('/')) || name == QStringLiteral(".."))
        return false;
    const QStringList parts = name.split(QLatin1Char('/'));
    return std::none_of(parts.cbegin(), parts.cend(), [](const QString &part) {
        return part.isEmpty() || part == QStringLiteral("..");
    });
}

// mkdir -p over raw bytes. A file that blocks the path is removed - it can only be one
// that the run being undone created. Returns false when a directory cannot be created.
bool makeDir(const QByteArray &path, QStringList *problems)
{
    if (path.isEmpty())
        return true;
    qsizetype slash = 0;
    while (slash < path.size()) {
        if (path.at(slash) == '/')
            ++slash;
        if (slash >= path.size())
            break;
        const qsizetype next = path.indexOf('/', slash);
        const QByteArray parent = next < 0 ? path : path.left(next);
        struct stat st;
        if (::lstat(parent.constData(), &st) == 0) {
            if (S_ISDIR(st.st_mode)) {
                if (next < 0)
                    return true;
                slash = next + 1;
                continue;
            }
            if (::unlink(parent.constData()) != 0) {
                if (problems)
                    *problems << QObject::tr("Cannot restore: %1 is in the way.")
                                  .arg(LocalPath::display(LocalPath::decode(parent)));
                return false;
            }
        }
        if (::mkdir(parent.constData(), 0755) != 0 && errno != EEXIST) {
            if (problems)
                *problems << QObject::tr("Cannot create the directory %1: %2")
                              .arg(LocalPath::display(LocalPath::decode(parent)),
                                   QString::fromLocal8Bit(::strerror(errno)));
            return false;
        }
        if (next < 0)
            return true;
        slash = next + 1;
    }
    return true;
}

// Removes a tree without following symlinks or entering other file systems (`device`).
// Returns false when an entry could not be removed (it and its parent stay in place).
// `cancel` is checked before every entry; `cancelled` reports that it stopped early.
bool removeTree(const QByteArray &path, dev_t device, int *removed, QStringList *problems,
                const std::atomic_bool *cancel, bool *cancelled)
{
    if (cancel && cancel->load()) {
        *cancelled = true;
        return false;
    }
    struct stat st;
    if (!lstatPath(path, &st))
        return true; // already gone
    if (st.st_dev != device) {
        if (problems)
            *problems << QObject::tr("Not removed (a different file system): %1")
                          .arg(LocalPath::display(LocalPath::decode(path)));
        return false;
    }
    if (S_ISDIR(st.st_mode)) {
        for (const Entry &entry : listDir(path)) {
            if (!removeTree(entry.path, device, removed, problems, cancel, cancelled))
                return false;
        }
        if (::rmdir(path.constData()) != 0) {
            if (problems)
                *problems << QObject::tr("Cannot remove the directory %1: %2")
                              .arg(LocalPath::display(LocalPath::decode(path)),
                                   QString::fromLocal8Bit(::strerror(errno)));
            return false;
        }
        ++*removed;
    } else {
        if (::unlink(path.constData()) != 0) {
            if (problems)
                *problems << QObject::tr("Cannot remove %1: %2")
                              .arg(LocalPath::display(LocalPath::decode(path)),
                                   QString::fromLocal8Bit(::strerror(errno)));
            return false;
        }
        ++*removed;
    }
    return true;
}

// Counts the entries of a tree (directories and files), without following symlinks.
int countTree(const QByteArray &path)
{
    struct stat st;
    if (!lstatPath(path, &st))
        return 0;
    if (!S_ISDIR(st.st_mode))
        return 1;
    int count = 1;
    for (const Entry &entry : listDir(path))
        count += countTree(entry.path);
    return count;
}

} // namespace

CopyUndo::Counts CopyUndo::count(const UndoRecord &record)
{
    Counts counts;
    if (!record.valid())
        return counts;
    struct stat st;
    const QByteArray undoDir = LocalPath::encode(record.undoDir());
    if (lstatPath(undoDir, &st)) {
        counts.backupDirExists = true;
        // The record file of the undo itself is not a backed-up old version.
        counts.restorations = countTree(undoDir) - 1;
        struct stat recordStat;
        if (lstatPath(QByteArray(undoDir + '/' + LocalPath::encode(recordFileName())), &recordStat))
            --counts.restorations;
    }
    const QByteArray dest = LocalPath::encode(record.destination);
    for (const QString &name : record.newEntries) {
        if (isSafeRelativeName(name) && ::lstat(QByteArray(dest + '/' + LocalPath::encode(name)).constData(), &st) == 0)
            ++counts.deletions;
    }
    return counts;
}

CopyUndo::Result CopyUndo::execute(const UndoRecord &record, const std::atomic_bool *cancel,
                                   const std::function<void(int, const QString &)> *progress)
{
    Result result;
    if (!record.valid()) {
        result.failed = 1;
        result.problems << QObject::tr("No copy to undo.");
        return result;
    }
    const std::function<void(int, const QString &)> report = [progress](int permille, const QString &status) {
        if (progress && *progress)
            (*progress)(permille, status);
    };

    const Counts counts = count(record);
    const int total = qMax(1, counts.deletions + counts.restorations);
    int done = 0;
    bool cancelled = false;

    // --- Delete the entries that the run created (deepest paths first, so that a
    // directory is empty by the time it is removed). ---
    struct stat destStat;
    if (lstatPath(LocalPath::encode(record.destination), &destStat)) {
        QStringList entries = record.newEntries;
        std::sort(entries.begin(), entries.end(), [](const QString &a, const QString &b) {
            return std::count(b.cbegin(), b.cend(), QLatin1Char('/')) > std::count(a.cbegin(), a.cend(), QLatin1Char('/'));
        });
        for (const QString &name : std::as_const(entries)) {
            if (cancel && cancel->load()) {
                result.cancelled = true;
                break;
            }
            if (!isSafeRelativeName(name)) {
                ++result.failed;
                result.problems << QObject::tr("Skipped (not a name below the destination): %1")
                                    .arg(LocalPath::display(name));
                continue;
            }
            const QByteArray path = LocalPath::encode(record.destination) + '/' + LocalPath::encode(name);
            if (::lstat(path.constData(), &destStat) != 0) // never created, e.g. the transfer stopped early
                continue;
            report(int(1000.0 * done / total), QObject::tr("Removing %1").arg(LocalPath::display(name)));
            int removed = 0;
            if (!removeTree(path, destStat.st_dev, &removed, &result.problems, cancel, &cancelled))
                ++result.failed;
            else
                result.deleted += removed;
            ++done;
            if (cancelled) {
                result.cancelled = true;
                break;
            }
        }
    }

    // --- Move the backed-up old versions back (deepest paths first, a whole directory
    // tree with one rename when the directory itself was deleted). ---
    const QByteArray undoDir = LocalPath::encode(record.undoDir());
    if (!result.cancelled && isRealDir(undoDir)) {
        // Restores `from` (inside the backup dir) to `to` (inside the destination).
        // Returns false when the entry or something below it could not be restored.
        // `root`: this call restores the backup dir itself - its record file is not a
        // backed-up old version, and it is deleted when everything else has moved back.
        const std::function<bool(const QByteArray &, const QByteArray &, int *, int *, bool)> restore =
            [&](const QByteArray &from, const QByteArray &to, int *done, int *restored, bool root) -> bool {
            if (cancel && cancel->load()) {
                result.cancelled = true;
                return false;
            }
            struct stat fromStat;
            if (!lstatPath(from, &fromStat))
                return true; // already restored
            const QByteArray toParent = to.left(to.lastIndexOf('/'));
            struct stat toStat;
            const bool toExists = lstatPath(to, &toStat);
            // Counted like CopyUndo::count(): a whole tree moved with one rename counts
            // as its entries, so that the confirmation and the result agree.
            int countAs = 1;
            if (!S_ISDIR(fromStat.st_mode)) {
                // A file or symlink: whatever the run left there is replaced.
                if (toExists && S_ISDIR(toStat.st_mode)) {
                    int removed = 0;
                    if (!removeTree(to, toStat.st_dev, &removed, &result.problems, cancel, &cancelled))
                        return false;
                } else {
                    if (toExists && ::unlink(to.constData()) != 0) {
                        ++result.failed;
                        result.problems << QObject::tr("Cannot replace %1: %2")
                                            .arg(LocalPath::display(LocalPath::decode(to)),
                                                 QString::fromLocal8Bit(::strerror(errno)));
                        return false;
                    }
                }
                if (!makeDir(toParent, &result.problems))
                    return false;
            } else if (!toExists) {
                // The whole tree with one rename.
                if (!makeDir(toParent, &result.problems))
                    return false;
                countAs = countTree(from);
            } else if (!S_ISDIR(toStat.st_mode)) {
                // A file that the run created where the old directory was.
                if (::unlink(to.constData()) != 0) {
                    ++result.failed;
                    result.problems << QObject::tr("Cannot restore the directory %1: %2 is in the way.")
                                        .arg(LocalPath::display(LocalPath::decode(to)),
                                             LocalPath::display(LocalPath::decode(to)));
                    return false;
                }
            } else {
                // Merge: the entries are moved back one by one, so that entries that the
                // run put there are replaced. Deeper paths first: the files of a restored
                // directory leave it empty by the time the directory itself is removed.
                for (const Entry &entry : listDir(from)) {
                    if (root && entry.name == recordFileName())
                        continue;
                    restore(entry.path, to + '/' + LocalPath::encode(entry.name), done, restored, false);
                    ++*done;
                    report(int(1000.0 * *done / total), QObject::tr("Restoring %1").arg(entry.name));
                    if (result.cancelled)
                        return false;
                }
                if (root)
                    ::unlink(QByteArray(from + '/' + LocalPath::encode(recordFileName())).constData());
                return ::rmdir(from.constData()) == 0;
            }
            if (::rename(from.constData(), to.constData()) != 0) {
                ++result.failed;
                result.problems << QObject::tr("Cannot restore %1: %2")
                                    .arg(LocalPath::display(LocalPath::decode(to)),
                                         QString::fromLocal8Bit(::strerror(errno)));
                return false;
            }
            *restored += countAs;
            *done += countAs;
            report(int(1000.0 * *done / total), QObject::tr("Restoring %1").arg(LocalPath::decode(to)));
            return true;
        };
        int restoreDone = done;
        restore(undoDir, LocalPath::encode(record.destination), &restoreDone, &result.restored, true);
        done = restoreDone;
        // Whatever could not be moved back stays in the backup dir.
        if (isRealDir(undoDir) && !listDir(undoDir).isEmpty()) {
            result.leftInBackupDir = record.undoDir();
            if (!result.cancelled)
                ++result.failed;
        }
    }

    report(result.cancelled ? 0 : 1000, QString());
    return result;
}

bool CopyUndo::discard(const UndoRecord &record, QStringList *problems)
{
    if (!record.valid())
        return true;
    const QByteArray undoDir = LocalPath::encode(record.undoDir());
    struct stat st;
    if (!lstatPath(undoDir, &st))
        return true;
    int removed = 0;
    bool cancelled = false;
    removeTree(undoDir, st.st_dev, &removed, problems, nullptr, &cancelled);
    struct stat after;
    return !lstatPath(undoDir, &after);
}

bool CopyUndo::save(const UndoRecord &record)
{
    if (!record.valid())
        return false;
    QJsonObject json;
    json.insert(QStringLiteral("destination"), record.destination);
    json.insert(QStringLiteral("undoDirName"), record.undoDirName);
    QJsonArray entries;
    for (const QString &name : record.newEntries)
        entries << name; // LocalPath keeps every byte as characters, JSON stays lossless
    json.insert(QStringLiteral("newEntries"), entries);
    json.insert(QStringLiteral("finishedAt"), QDateTime::currentDateTime().toString(Qt::ISODate));

    QDir().mkpath(record.undoDir());
    QFile file(record.recordPath());
    if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate))
        return false;
    file.write(QJsonDocument(json).toJson(QJsonDocument::Compact));
    return true;
}

UndoRecord CopyUndo::load(const QString &recordPath, bool *ok)
{
    UndoRecord record;
    if (ok)
        *ok = false;
    QFile file(recordPath);
    if (!file.open(QIODevice::ReadOnly))
        return record;
    const QJsonObject json = QJsonDocument::fromJson(file.readAll()).object();
    record.destination = json.value(QStringLiteral("destination")).toString();
    record.undoDirName = json.value(QStringLiteral("undoDirName")).toString();
    record.newEntries.clear();
    for (const QJsonValue &value : json.value(QStringLiteral("newEntries")).toArray())
        record.newEntries << value.toString();
    if (ok)
        *ok = record.valid() && recordPath == record.recordPath();
    return record;
}
