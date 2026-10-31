#pragma once

#include <QString>

class QIODevice;

// Helpers for "copy only the files tracked by Git".
namespace GitSource {

// Whether `dir` is the top level of a non-bare Git work tree. On failure `error` explains why.
bool isRepositoryRoot(const QString &dir, QString *error = nullptr);

// Writes the NUL-separated file list for `--files-from`/`--from0`:
// every path from `git ls-files`, and (if includeRepository) every entry under .git.
// The paths are relative to `repo`, which must be a repository top level.
bool writeTrackedFileList(const QString &repo, bool includeRepository, QIODevice *out,
                          QString *error = nullptr);

} // namespace GitSource
