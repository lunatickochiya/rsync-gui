#include "localpath.h"
#include "mainwindow.h"
#include "rsyncoptions.h"

#include <QApplication>
#include <QCommandLineParser>
#include <QLibraryInfo>
#include <QLocale>
#include <QTimer>
#include <QTranslator>

#include <cstdio>

int main(int argc, char *argv[])
{
    QApplication app(argc, argv);

    // This is the Simplified Chinese edition. The application translation is
    // embedded as a Qt resource; Qt's own strings (standard dialogs, help
    // headers) come from qt6-translations-l10n when it is installed.
    QTranslator qtTranslator;
    if (qtTranslator.load(QLocale(QLocale::Chinese, QLocale::China), QStringLiteral("qt"),
                          QStringLiteral("_"),
                          QLibraryInfo::path(QLibraryInfo::TranslationsPath))) {
        QApplication::installTranslator(&qtTranslator);
    }
    QTranslator translator;
    if (translator.load(QStringLiteral(":/i18n/rsync-gui_zh_CN.qm")))
        QApplication::installTranslator(&translator);

    QApplication::setOrganizationName(QStringLiteral("fapped"));
    QApplication::setApplicationName(QStringLiteral("rsync-gui"));
    QApplication::setApplicationDisplayName(QStringLiteral("Rsync GUI 中文版"));
    QApplication::setApplicationVersion(QStringLiteral(RSYNC_GUI_VERSION));
    QApplication::setDesktopFileName(QStringLiteral("rsync-gui"));

    QCommandLineParser parser;
    parser.setApplicationDescription(QStringLiteral(
        "使用 rsync 复制文件的图形前端。\n\n"
        "通过 --preset 或 --config 加载的选项仅用于本次会话，"
        "不会替换已记住的选项。"));
    parser.addHelpOption();
    parser.addVersionOption();
    const QCommandLineOption presetOption({QStringLiteral("p"), QStringLiteral("preset")},
                                          QStringLiteral("加载预设 <name>（见 --list-presets）。"),
                                          QStringLiteral("name"));
    const QCommandLineOption configOption({QStringLiteral("c"), QStringLiteral("config")},
                                          QStringLiteral("从 JSON 设置文件 <file> 加载选项。"),
                                          QStringLiteral("file"));
    const QCommandLineOption destinationOption({QStringLiteral("d"), QStringLiteral("destination")},
                                               QStringLiteral("设置目标目录。"),
                                               QStringLiteral("dir"));
    const QCommandLineOption browseOption(QStringLiteral("browse-destination"),
                                          QStringLiteral("启动时打开选择目标目录的对话框。"));
    const QCommandLineOption listPresetsOption(QStringLiteral("list-presets"),
                                               QStringLiteral("列出可用预设并退出。"));
    parser.addOption(presetOption);
    parser.addOption(configOption);
    parser.addOption(destinationOption);
    parser.addOption(browseOption);
    parser.addOption(listPresetsOption);
    parser.addPositionalArgument(QStringLiteral("sources"),
                                 QStringLiteral("要复制的文件或目录（路径、file:// 或 sftp:// URL）。"),
                                 QStringLiteral("[源...]"));
    // QCoreApplication::arguments() would replace bytes that are not valid UTF-8 in file names;
    // the raw argv (Qt's own options already removed by QApplication) is decoded losslessly.
    QStringList arguments;
    for (int i = 0; i < argc; ++i)
        arguments << LocalPath::decode(QByteArray(argv[i]));
    parser.process(arguments);

    if (parser.isSet(listPresetsOption)) {
        const QList<RsyncPreset> presets = RsyncOptions::presets();
        for (const RsyncPreset &preset : presets)
            std::printf("%s\t%s\t%s\n", qPrintable(preset.id), qPrintable(preset.label), qPrintable(preset.file));
        if (presets.isEmpty()) {
            std::fprintf(stderr, "未找到预设。已搜索：\n%s\n",
                         qPrintable(RsyncOptions::presetSearchPaths().join(QLatin1Char('\n'))));
        }
        return 0;
    }

    MainWindow window;
    window.show();
    if (parser.isSet(presetOption))
        window.loadPreset(parser.value(presetOption));
    if (parser.isSet(configOption))
        window.importSettingsFile(parser.value(configOption), MainWindow::Persistence::SessionOnly);
    window.addSources(parser.positionalArguments());
    if (parser.isSet(destinationOption))
        window.setDestination(parser.value(destinationOption));
    if (parser.isSet(browseOption))
        QTimer::singleShot(0, &window, &MainWindow::browseDestination);
    return app.exec();
}
