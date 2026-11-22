#pragma once

#include <QString>
#include <QStringList>

#include <atomic>
#include <functional>

// Undo data of one copy run into a local destination (see RsyncOptions::undoDirName):
// the run added --backup --backup-dir=<undoDirName> --itemize-changes, so files that it
// overwrote or deleted were moved into that directory inside the destination (mirroring
// their relative paths), and RsyncJob recorded every entry that it created new.
// CopyUndo restores the destination to its state before the run: the recorded new
// entries are deleted, the backed-up old versions are moved back.
struct UndoRecord
{
    // The destination directory (a local path, no trailing "/") and the name of the
    // backup directory inside it.
    QString destination;
    QString undoDirName;
    // Entries created by the run, relative to the destination (as rsync printed them,
    // without a trailing "/").
    QStringList newEntries;

    bool valid() const { return !destination.isEmpty() && !undoDirName.isEmpty(); }
    // destination/undoDirName
    QString undoDir() const;
    // The file this record is stored in (inside the backup dir), so that a later
    // session of the application can still undo the run.
    QString recordPath() const;
    void clear() { destination.clear(); undoDirName.clear(); newEntries.clear(); }
};

class CopyUndo
{
public:
    // The name of the record file that save() stores inside the backup dir.
    static QString recordFileName();

    struct Counts
    {
        int deletions = 0;   // recorded entries that exist in the destination
        int restorations = 0; // entries in the backup dir (old versions to move back)
        bool backupDirExists = false;
    };
    // What execute() would do; for a confirmation dialog. The backup dir contains only
    // the changed files, so walking it is cheap.
    static Counts count(const UndoRecord &record);

    struct Result
    {
        int deleted = 0;     // entries created by the run that were removed
        int restored = 0;    // old versions that were moved back
        int failed = 0;      // entries that could not be deleted or restored
        QStringList problems; // human-readable errors, one line each
        // Old versions that could not be restored - they are still in this directory.
        QString leftInBackupDir;
        bool cancelled = false;
        bool ok() const { return !cancelled && failed == 0; }
    };

    // Restores the destination to its state before the copy: removes the entries that
    // the run created and moves the backed-up old versions back (deepest paths first,
    // so that a directory is empty by the time it is removed). Nothing outside the
    // destination is touched: symlinks are not followed and other file systems (mount
    // points inside a created directory) are not entered. `cancel` is checked between
    // entries; `progress` (optional) is called with a permille value and a status line.
    static Result execute(const UndoRecord &record, const std::atomic_bool *cancel = nullptr,
                          const std::function<void(int, const QString &)> *progress = nullptr);

    // Deletes the backup dir without undoing anything (before the next copy run).
    // `problems` (optional) receives one line per entry that could not be removed.
    static bool discard(const UndoRecord &record, QStringList *problems = nullptr);

    // Persists the record in its backup dir (see recordPath()).
    static bool save(const UndoRecord &record);
    // Reads a record written by save(); `ok` (optional) is false when there is none.
    static UndoRecord load(const QString &recordPath, bool *ok = nullptr);
};
